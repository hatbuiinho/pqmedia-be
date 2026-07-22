package service

import (
	"context"
	"fmt"
	"io"
	"net/mail"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"

	"pqmedia/be/internal/auth"
	"pqmedia/be/internal/repository"
)

const (
	userImportPreviewTTL     = 15 * time.Minute
	userImportMaxFileSize    = 10 << 20
	userImportExpectedSheet  = 0
	userImportHeaderRowIndex = 1
)

type UserImportPreview struct {
	ImportID  string
	ExpiresAt time.Time
	Summary   UserImportSummary
	Rows      []UserImportPreviewRow
}

type UserImportSummary struct {
	TotalRows        int
	CreateCount      int
	UpdateCount      int
	SkipCount        int
	ErrorCount       int
	PasswordSetCount int
}

type UserImportPreviewRow struct {
	RowNumber      int
	Email          string
	FullName       string
	DharmaName     *string
	BirthYear      *int16
	Phone          *string
	CTN            *string
	Action         string
	Message        string
	PasswordAction string
	Changes        UserImportRowChanges
}

type UserImportRowChanges struct {
	FullName              bool
	DharmaName            bool
	BirthYear             bool
	Phone                 bool
	CTN                   bool
	IsAdmin               bool
	CanManagePublications bool
	IsActive              bool
	Password              bool
}

type CommitUserImportResult struct {
	Summary UserImportSummary
}

type CommitUserImportInput struct {
	ImportID string
}

type userImportParsedRow struct {
	RowNumber             int
	Email                 string
	FullName              string
	DharmaName            *string
	BirthYear             *int16
	Phone                 *string
	CTN                   *string
	Password              string
	IsAdmin               *bool
	CanManagePublications *bool
	IsActive              *bool
	ParseError            string
}

type userImportPreparedRow struct {
	preview   UserImportPreviewRow
	apply     *repository.ImportUserParams
	beforeAAD bool
	afterAAD  bool
}

type userImportSession struct {
	ActorUserID uuid.UUID
	ExpiresAt   time.Time
	Rows        []UserImportPreviewRow
	Operations  []repository.ImportUserParams
	Summary     UserImportSummary
}

var userImportHeaders = map[string]string{
	"email":                   "email",
	"full_name":               "full_name",
	"dharma_name":             "dharma_name",
	"birth_year":              "birth_year",
	"phone":                   "phone",
	"ctn":                     "ctn",
	"password":                "password",
	"is_admin":                "is_admin",
	"can_manage_publications": "can_manage_publications",
	"is_active":               "is_active",
}

func (s *UserService) PreviewUserImport(ctx context.Context, actor Principal, file io.Reader) (UserImportPreview, error) {
	if !actor.User.IsAdmin {
		return UserImportPreview{}, ErrForbidden
	}

	parsedRows, err := parseUserImportWorkbook(file)
	if err != nil {
		return UserImportPreview{}, err
	}
	if len(parsedRows) == 0 {
		return UserImportPreview{}, ValidationError("file import không có dòng dữ liệu")
	}

	prepared, err := s.prepareUserImportRows(ctx, parsedRows)
	if err != nil {
		return UserImportPreview{}, err
	}

	rows := make([]UserImportPreviewRow, 0, len(prepared))
	ops := make([]repository.ImportUserParams, 0, len(prepared))
	summary := UserImportSummary{TotalRows: len(prepared)}
	for _, item := range prepared {
		rows = append(rows, item.preview)
		switch item.preview.Action {
		case "create":
			summary.CreateCount++
			ops = append(ops, *item.apply)
		case "update":
			summary.UpdateCount++
			ops = append(ops, *item.apply)
		case "skip":
			summary.SkipCount++
		case "error":
			summary.ErrorCount++
		}
		if item.preview.PasswordAction == "set" || item.preview.PasswordAction == "update" {
			summary.PasswordSetCount++
		}
	}

	importID := uuid.NewString()
	expiresAt := s.now().Add(userImportPreviewTTL)
	s.storeUserImportSession(importID, userImportSession{
		ActorUserID: actor.User.ID,
		ExpiresAt:   expiresAt,
		Rows:        rows,
		Operations:  ops,
		Summary:     summary,
	})

	return UserImportPreview{
		ImportID:  importID,
		ExpiresAt: expiresAt,
		Summary:   summary,
		Rows:      rows,
	}, nil
}

func (s *UserService) CommitUserImport(ctx context.Context, actor Principal, input CommitUserImportInput) (CommitUserImportResult, error) {
	if !actor.User.IsAdmin {
		return CommitUserImportResult{}, ErrForbidden
	}
	importID := strings.TrimSpace(input.ImportID)
	if importID == "" {
		return CommitUserImportResult{}, ValidationError("import_id is required")
	}

	session, ok := s.loadUserImportSession(importID, actor.User.ID)
	if !ok {
		return CommitUserImportResult{}, NewError(400, "import_preview_expired", "phiên xem trước import đã hết hạn hoặc không hợp lệ")
	}
	if session.Summary.ErrorCount > 0 {
		return CommitUserImportResult{}, ValidationError("preview đang có lỗi, chưa thể import")
	}

	if err := s.validateImportOperations(ctx, session.Operations); err != nil {
		return CommitUserImportResult{}, err
	}
	if err := s.Repo.ImportUsers(ctx, session.Operations); err != nil {
		return CommitUserImportResult{}, err
	}

	s.deleteUserImportSession(importID)
	return CommitUserImportResult{Summary: session.Summary}, nil
}

func (s *UserService) prepareUserImportRows(ctx context.Context, rows []userImportParsedRow) ([]userImportPreparedRow, error) {
	emails := make([]string, 0, len(rows))
	seenEmails := make(map[string]struct{}, len(rows))
	duplicateRows := make(map[string][]int)
	for _, row := range rows {
		if row.Email == "" {
			continue
		}
		duplicateRows[row.Email] = append(duplicateRows[row.Email], row.RowNumber)
		if _, ok := seenEmails[row.Email]; ok {
			continue
		}
		seenEmails[row.Email] = struct{}{}
		emails = append(emails, row.Email)
	}

	existingUsers, err := s.Repo.ListUsersByEmails(ctx, emails)
	if err != nil {
		return nil, err
	}
	existingByEmail := make(map[string]repository.UserWithProfile, len(existingUsers))
	for _, item := range existingUsers {
		existingByEmail[item.User.Email] = item
	}

	currentActiveAdmins, err := s.Repo.CountActiveAdmins(ctx)
	if err != nil {
		return nil, err
	}

	prepared := make([]userImportPreparedRow, 0, len(rows))
	activeAdminDelta := 0

	for _, row := range rows {
		if row.ParseError != "" {
			prepared = append(prepared, userImportPreparedRow{
				preview: UserImportPreviewRow{
					RowNumber:  row.RowNumber,
					Email:      row.Email,
					FullName:   row.FullName,
					DharmaName: row.DharmaName,
					BirthYear:  row.BirthYear,
					Phone:      row.Phone,
					CTN:        row.CTN,
					Action:     "error",
					Message:    row.ParseError,
				},
			})
			continue
		}

		if duplicates := duplicateRows[row.Email]; row.Email != "" && len(duplicates) > 1 {
			prepared = append(prepared, userImportPreparedRow{
				preview: UserImportPreviewRow{
					RowNumber:  row.RowNumber,
					Email:      row.Email,
					FullName:   row.FullName,
					DharmaName: row.DharmaName,
					BirthYear:  row.BirthYear,
					Phone:      row.Phone,
					CTN:        row.CTN,
					Action:     "error",
					Message:    fmt.Sprintf("email bị lặp trong file import ở các dòng %s", joinRowNumbers(duplicates)),
				},
			})
			continue
		}

		item, err := s.prepareUserImportRow(row, existingByEmail[row.Email])
		if err != nil {
			prepared = append(prepared, userImportPreparedRow{
				preview: UserImportPreviewRow{
					RowNumber:  row.RowNumber,
					Email:      row.Email,
					FullName:   row.FullName,
					DharmaName: row.DharmaName,
					BirthYear:  row.BirthYear,
					Phone:      row.Phone,
					CTN:        row.CTN,
					Action:     "error",
					Message:    err.Error(),
				},
			})
			continue
		}

		if item.apply != nil {
			activeAdminDelta += boolDelta(item.beforeAAD, item.afterAAD)
		}
		prepared = append(prepared, item)
	}

	if currentActiveAdmins+activeAdminDelta < 1 {
		for i := range prepared {
			if prepared[i].apply == nil || !prepared[i].beforeAAD || prepared[i].afterAAD {
				continue
			}
			prepared[i].apply = nil
			prepared[i].preview.Action = "error"
			prepared[i].preview.Message = "import phải giữ lại ít nhất một admin đang hoạt động"
		}
	}

	return prepared, nil
}

func (s *UserService) prepareUserImportRow(row userImportParsedRow, existing repository.UserWithProfile) (userImportPreparedRow, error) {
	if row.Email == "" {
		return userImportPreparedRow{}, ValidationError("email is required")
	}
	if _, err := mail.ParseAddress(row.Email); err != nil {
		return userImportPreparedRow{}, ValidationError("invalid email")
	}
	if row.FullName == "" {
		return userImportPreparedRow{}, ValidationError("full_name is required")
	}
	if row.Password != "" && len(row.Password) < 8 {
		return userImportPreparedRow{}, ValidationError("password must be at least 8 characters")
	}
	profileInput, err := s.normalizeProfileInput(UpdateProfileInput{
		FullName:   row.FullName,
		DharmaName: row.DharmaName,
		BirthYear:  row.BirthYear,
		Phone:      row.Phone,
		CTN:        row.CTN,
	})
	if err != nil {
		return userImportPreparedRow{}, err
	}

	hasExisting := existing.User.ID != uuid.Nil
	if !hasExisting && row.Password == "" {
		return userImportPreparedRow{}, ValidationError("password is required when creating a new user")
	}

	if !hasExisting {
		passwordHash, err := auth.HashPassword(row.Password)
		if err != nil {
			return userImportPreparedRow{}, NewError(500, "hash_password_failed", err.Error())
		}
		isAdmin := false
		if row.IsAdmin != nil {
			isAdmin = *row.IsAdmin
		}
		canManage := false
		if row.CanManagePublications != nil {
			canManage = *row.CanManagePublications
		}
		isActive := true
		if row.IsActive != nil {
			isActive = *row.IsActive
		}
		return userImportPreparedRow{
			preview: UserImportPreviewRow{
				RowNumber:      row.RowNumber,
				Email:          row.Email,
				FullName:       profileInput.FullName,
				DharmaName:     profileInput.DharmaName,
				BirthYear:      profileInput.BirthYear,
				Phone:          profileInput.Phone,
				CTN:            profileInput.CTN,
				Action:         "create",
				PasswordAction: "set",
				Changes: UserImportRowChanges{
					FullName:              true,
					DharmaName:            profileInput.DharmaName != nil,
					BirthYear:             profileInput.BirthYear != nil,
					Phone:                 profileInput.Phone != nil,
					CTN:                   profileInput.CTN != nil,
					IsAdmin:               isAdmin,
					CanManagePublications: canManage,
					IsActive:              !isActive,
					Password:              true,
				},
			},
			apply: &repository.ImportUserParams{
				Email:                 row.Email,
				PasswordHash:          &passwordHash,
				FullName:              profileInput.FullName,
				DharmaName:            profileInput.DharmaName,
				BirthYear:             profileInput.BirthYear,
				Phone:                 profileInput.Phone,
				CTN:                   profileInput.CTN,
				IsAdmin:               isAdmin,
				CanManagePublications: canManage,
				IsActive:              isActive,
			},
			beforeAAD: false,
			afterAAD:  isAdmin && isActive,
		}, nil
	}

	isAdmin := existing.User.IsAdmin
	if row.IsAdmin != nil {
		isAdmin = *row.IsAdmin
	}
	canManage := existing.User.CanManagePublications
	if row.CanManagePublications != nil {
		canManage = *row.CanManagePublications
	}
	isActive := existing.User.IsActive
	if row.IsActive != nil {
		isActive = *row.IsActive
	}

	var passwordHash *string
	passwordAction := "keep"
	passwordChanged := false
	if row.Password != "" {
		hashed, err := auth.HashPassword(row.Password)
		if err != nil {
			return userImportPreparedRow{}, NewError(500, "hash_password_failed", err.Error())
		}
		passwordHash = &hashed
		passwordAction = "update"
		passwordChanged = true
	}

	changes := UserImportRowChanges{
		FullName:              profileInput.FullName != existing.Profile.FullName,
		DharmaName:            stringPtrValue(profileInput.DharmaName) != stringPtrValue(existing.Profile.DharmaName),
		BirthYear:             int16PtrValue(profileInput.BirthYear) != int16PtrValue(existing.Profile.BirthYear),
		Phone:                 stringPtrValue(profileInput.Phone) != stringPtrValue(existing.Profile.Phone),
		CTN:                   stringPtrValue(profileInput.CTN) != stringPtrValue(existing.Profile.CTN),
		IsAdmin:               isAdmin != existing.User.IsAdmin,
		CanManagePublications: canManage != existing.User.CanManagePublications,
		IsActive:              isActive != existing.User.IsActive,
		Password:              passwordChanged,
	}
	if !hasAnyImportChange(changes) {
		return userImportPreparedRow{
			preview: UserImportPreviewRow{
				RowNumber:      row.RowNumber,
				Email:          row.Email,
				FullName:       profileInput.FullName,
				DharmaName:     profileInput.DharmaName,
				BirthYear:      profileInput.BirthYear,
				Phone:          profileInput.Phone,
				CTN:            profileInput.CTN,
				Action:         "skip",
				Message:        "không có thay đổi",
				PasswordAction: passwordAction,
				Changes:        changes,
			},
		}, nil
	}

	existingID := existing.User.ID
	return userImportPreparedRow{
		preview: UserImportPreviewRow{
			RowNumber:      row.RowNumber,
			Email:          row.Email,
			FullName:       profileInput.FullName,
			DharmaName:     profileInput.DharmaName,
			BirthYear:      profileInput.BirthYear,
			Phone:          profileInput.Phone,
			CTN:            profileInput.CTN,
			Action:         "update",
			PasswordAction: passwordAction,
			Changes:        changes,
		},
		apply: &repository.ImportUserParams{
			ExistingUserID:        &existingID,
			Email:                 row.Email,
			PasswordHash:          passwordHash,
			FullName:              profileInput.FullName,
			DharmaName:            profileInput.DharmaName,
			BirthYear:             profileInput.BirthYear,
			Phone:                 profileInput.Phone,
			CTN:                   profileInput.CTN,
			IsAdmin:               isAdmin,
			CanManagePublications: canManage,
			IsActive:              isActive,
		},
		beforeAAD: existing.User.IsAdmin && existing.User.IsActive,
		afterAAD:  isAdmin && isActive,
	}, nil
}

func (s *UserService) validateImportOperations(ctx context.Context, ops []repository.ImportUserParams) error {
	if len(ops) == 0 {
		return nil
	}

	currentActiveAdmins, err := s.Repo.CountActiveAdmins(ctx)
	if err != nil {
		return err
	}
	existingIDs := make([]uuid.UUID, 0, len(ops))
	for _, op := range ops {
		if op.ExistingUserID != nil {
			existingIDs = append(existingIDs, *op.ExistingUserID)
		}
	}
	existingUsers, err := s.Repo.ListUsersByIDs(ctx, existingIDs)
	if err != nil {
		return err
	}
	existingByID := make(map[uuid.UUID]repository.User, len(existingUsers))
	for _, user := range existingUsers {
		existingByID[user.ID] = user
	}

	activeAdminDelta := 0
	for _, op := range ops {
		before := false
		if op.ExistingUserID != nil {
			existing, ok := existingByID[*op.ExistingUserID]
			if !ok {
				return ErrNotFound
			}
			before = existing.IsAdmin && existing.IsActive
		}
		after := op.IsAdmin && op.IsActive
		activeAdminDelta += boolDelta(before, after)
	}
	if currentActiveAdmins+activeAdminDelta < 1 {
		return ValidationError("import phải giữ lại ít nhất một admin đang hoạt động")
	}
	return nil
}

func parseUserImportWorkbook(file io.Reader) ([]userImportParsedRow, error) {
	workbook, err := excelize.OpenReader(file)
	if err != nil {
		return nil, ValidationError("không đọc được file Excel, vui lòng dùng file .xlsx hợp lệ")
	}
	defer func() { _ = workbook.Close() }()

	sheets := workbook.GetSheetList()
	if len(sheets) <= userImportExpectedSheet {
		return nil, ValidationError("file Excel không có sheet dữ liệu")
	}

	rows, err := workbook.GetRows(sheets[userImportExpectedSheet])
	if err != nil {
		return nil, ValidationError("không đọc được dữ liệu từ sheet đầu tiên")
	}
	if len(rows) < userImportHeaderRowIndex {
		return nil, ValidationError("file Excel chưa có hàng tiêu đề")
	}

	headerMap, err := buildUserImportHeaderMap(rows[0])
	if err != nil {
		return nil, err
	}

	out := make([]userImportParsedRow, 0, max(0, len(rows)-1))
	for i := 1; i < len(rows); i++ {
		rowValues := rows[i]
		if isExcelRowEmpty(rowValues) {
			continue
		}

		parsed, err := parseUserImportRow(i+1, rowValues, headerMap)
		if err != nil {
			out = append(out, userImportParsedRow{
				RowNumber:  i + 1,
				Email:      normalizeUserImportText(cellValue(rowValues, headerMap, "email")),
				FullName:   normalizeUserImportText(cellValue(rowValues, headerMap, "full_name")),
				DharmaName: nullableImportText(cellValue(rowValues, headerMap, "dharma_name")),
				BirthYear:  parseBirthYearFallback(cellValue(rowValues, headerMap, "birth_year")),
				Phone:      nullableImportText(cellValue(rowValues, headerMap, "phone")),
				CTN:        nullableImportText(cellValue(rowValues, headerMap, "ctn")),
				Password:   normalizeUserImportText(cellValue(rowValues, headerMap, "password")),
				ParseError: err.Error(),
			})
			continue
		}
		out = append(out, parsed)
	}
	return out, nil
}

func buildUserImportHeaderMap(headers []string) (map[string]int, error) {
	indexByField := make(map[string]int, len(userImportHeaders))
	for i, header := range headers {
		normalized := normalizeUserImportHeader(header)
		field, ok := userImportHeaders[normalized]
		if !ok {
			continue
		}
		indexByField[field] = i
	}

	for _, required := range []string{"email", "full_name", "password"} {
		if _, ok := indexByField[required]; ok {
			continue
		}
		if required == "password" {
			return nil, ValidationError("thiếu cột password trong file import")
		}
		return nil, ValidationError(fmt.Sprintf("thiếu cột %s trong file import", required))
	}
	return indexByField, nil
}

func parseUserImportRow(rowNumber int, values []string, headerMap map[string]int) (userImportParsedRow, error) {
	isAdmin, err := parseOptionalImportBool(cellValue(values, headerMap, "is_admin"))
	if err != nil {
		return userImportParsedRow{}, fmt.Errorf("cột is_admin: %w", err)
	}
	canManage, err := parseOptionalImportBool(cellValue(values, headerMap, "can_manage_publications"))
	if err != nil {
		return userImportParsedRow{}, fmt.Errorf("cột can_manage_publications: %w", err)
	}
	isActive, err := parseOptionalImportBool(cellValue(values, headerMap, "is_active"))
	if err != nil {
		return userImportParsedRow{}, fmt.Errorf("cột is_active: %w", err)
	}

	email := normalizeImportEmail(cellValue(values, headerMap, "email"))
	fullName := normalizeUserImportText(cellValue(values, headerMap, "full_name"))
	dharmaName := nullableImportText(cellValue(values, headerMap, "dharma_name"))
	birthYear, err := parseOptionalImportBirthYear(cellValue(values, headerMap, "birth_year"))
	if err != nil {
		return userImportParsedRow{}, fmt.Errorf("cột birth_year: %w", err)
	}
	password := normalizeUserImportText(cellValue(values, headerMap, "password"))
	phone := nullableImportText(cellValue(values, headerMap, "phone"))
	ctn := nullableImportText(cellValue(values, headerMap, "ctn"))

	return userImportParsedRow{
		RowNumber:             rowNumber,
		Email:                 email,
		FullName:              fullName,
		DharmaName:            dharmaName,
		BirthYear:             birthYear,
		Phone:                 phone,
		CTN:                   ctn,
		Password:              password,
		IsAdmin:               isAdmin,
		CanManagePublications: canManage,
		IsActive:              isActive,
	}, nil
}

func parseOptionalImportBool(raw string) (*bool, error) {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		return nil, nil
	}
	switch value {
	case "1", "true", "yes", "y", "co", "có", "x":
		v := true
		return &v, nil
	case "0", "false", "no", "n", "khong", "không":
		v := false
		return &v, nil
	default:
		return nil, fmt.Errorf("giá trị không hợp lệ")
	}
}

func parseOptionalImportBirthYear(raw string) (*int16, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, nil
	}
	year, err := strconv.Atoi(value)
	if err != nil {
		return nil, fmt.Errorf("giá trị không hợp lệ")
	}
	if year < minBirthYear || year > 32767 {
		return nil, fmt.Errorf("giá trị không hợp lệ")
	}
	year16 := int16(year)
	return &year16, nil
}

func (s *UserService) storeUserImportSession(importID string, session userImportSession) {
	s.importSessionsMu.Lock()
	defer s.importSessionsMu.Unlock()

	s.cleanupExpiredUserImportSessionsLocked()
	if s.importSessions == nil {
		s.importSessions = make(map[string]userImportSession)
	}
	s.importSessions[importID] = session
}

func (s *UserService) loadUserImportSession(importID string, actorUserID uuid.UUID) (userImportSession, bool) {
	s.importSessionsMu.Lock()
	defer s.importSessionsMu.Unlock()

	s.cleanupExpiredUserImportSessionsLocked()
	session, ok := s.importSessions[importID]
	if !ok || session.ActorUserID != actorUserID {
		return userImportSession{}, false
	}
	return session, true
}

func (s *UserService) deleteUserImportSession(importID string) {
	s.importSessionsMu.Lock()
	defer s.importSessionsMu.Unlock()
	delete(s.importSessions, importID)
}

func (s *UserService) cleanupExpiredUserImportSessionsLocked() {
	if len(s.importSessions) == 0 {
		return
	}
	now := s.now()
	for importID, session := range s.importSessions {
		if session.ExpiresAt.After(now) {
			continue
		}
		delete(s.importSessions, importID)
	}
}

func cellValue(values []string, headerMap map[string]int, field string) string {
	index, ok := headerMap[field]
	if !ok || index < 0 || index >= len(values) {
		return ""
	}
	return values[index]
}

func isExcelRowEmpty(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

func normalizeUserImportHeader(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeUserImportText(value string) string {
	return strings.TrimSpace(value)
}

func nullableImportText(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizeImportEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func int16PtrValue(value *int16) int16 {
	if value == nil {
		return 0
	}
	return *value
}

func parseBirthYearFallback(raw string) *int16 {
	year, err := parseOptionalImportBirthYear(raw)
	if err != nil {
		return nil
	}
	return year
}

func hasAnyImportChange(changes UserImportRowChanges) bool {
	return changes.FullName ||
		changes.DharmaName ||
		changes.BirthYear ||
		changes.Phone ||
		changes.CTN ||
		changes.IsAdmin ||
		changes.CanManagePublications ||
		changes.IsActive ||
		changes.Password
}

func boolDelta(before, after bool) int {
	switch {
	case before == after:
		return 0
	case !before && after:
		return 1
	default:
		return -1
	}
}

func joinRowNumbers(rows []int) string {
	if len(rows) == 0 {
		return ""
	}
	sorted := slices.Clone(rows)
	slices.Sort(sorted)
	parts := make([]string, 0, len(sorted))
	for _, row := range sorted {
		parts = append(parts, fmt.Sprintf("%d", row))
	}
	return strings.Join(parts, ", ")
}
