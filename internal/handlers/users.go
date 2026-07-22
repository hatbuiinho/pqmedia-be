package handlers

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"

	"pqmedia/be/internal/authctx"
	"pqmedia/be/internal/httpx"
	"pqmedia/be/internal/service"
)

type UserHandler struct {
	Service *service.UserService
}

type createUserRequest struct {
	Email                 string  `json:"email"`
	Password              string  `json:"password"`
	FullName              string  `json:"full_name"`
	DharmaName            *string `json:"dharma_name"`
	BirthYear             *int16  `json:"birth_year"`
	Phone                 *string `json:"phone"`
	CTN                   *string `json:"ctn"`
	IsAdmin               bool    `json:"is_admin"`
	CanManagePublications bool    `json:"can_manage_publications"`
	IsActive              *bool   `json:"is_active"`
}

type updateProfileRequest struct {
	FullName   string  `json:"full_name"`
	DharmaName *string `json:"dharma_name"`
	BirthYear  *int16  `json:"birth_year"`
	Phone      *string `json:"phone"`
	CTN        *string `json:"ctn"`
}

type updateUserRequest struct {
	FullName              string  `json:"full_name"`
	DharmaName            *string `json:"dharma_name"`
	BirthYear             *int16  `json:"birth_year"`
	Phone                 *string `json:"phone"`
	CTN                   *string `json:"ctn"`
	IsAdmin               bool    `json:"is_admin"`
	CanManagePublications bool    `json:"can_manage_publications"`
	IsActive              bool    `json:"is_active"`
}

type resetUserPasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	Password        string `json:"password"`
}

type commitUserImportRequest struct {
	ImportID string `json:"import_id"`
}

type listResponse struct {
	Items []PrincipalDTO `json:"items"`
	Page  PageMetaDTO    `json:"page"`
}

type userImportCommitResponse struct {
	Summary UserImportSummaryDTO `json:"summary"`
}

func (h UserHandler) List(w http.ResponseWriter, r *http.Request) {
	actor := authctx.MustPrincipal(r.Context())
	q := r.URL.Query().Get("q")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	items, page, err := h.Service.ListUsers(r.Context(), actor, q, limit, offset)
	if err != nil {
		WriteServiceError(w, err)
		return
	}

	dtos := make([]PrincipalDTO, 0, len(items))
	for _, p := range items {
		dtos = append(dtos, ToPrincipal(p, nil))
	}
	httpx.WriteJSON(w, http.StatusOK, listResponse{Items: dtos, Page: ToPageMeta(page)})
}

func (h UserHandler) Create(w http.ResponseWriter, r *http.Request) {
	actor := authctx.MustPrincipal(r.Context())
	var body createUserRequest
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	created, err := h.Service.CreateUser(r.Context(), actor, service.CreateUserInput{
		Email:                 body.Email,
		Password:              body.Password,
		FullName:              body.FullName,
		DharmaName:            body.DharmaName,
		BirthYear:             body.BirthYear,
		Phone:                 body.Phone,
		CTN:                   body.CTN,
		IsAdmin:               body.IsAdmin,
		CanManagePublications: body.CanManagePublications,
		IsActive:              body.IsActive == nil || *body.IsActive,
	})
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, ToPrincipal(created, nil))
}

func (h UserHandler) PreviewImport(w http.ResponseWriter, r *http.Request) {
	actor := authctx.MustPrincipal(r.Context())

	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_multipart", "không đọc được file import")
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		if errors.Is(err, http.ErrMissingFile) {
			httpx.WriteError(w, http.StatusBadRequest, "missing_file", "vui lòng chọn file Excel để import")
			return
		}
		httpx.WriteError(w, http.StatusBadRequest, "invalid_file", err.Error())
		return
	}
	defer func() { _ = file.Close() }()

	preview, err := h.Service.PreviewUserImport(r.Context(), actor, file)
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toUserImportPreviewDTO(preview))
}

func (h UserHandler) DownloadImportTemplate(w http.ResponseWriter, r *http.Request) {
	actor := authctx.MustPrincipal(r.Context())
	if !actor.User.IsAdmin {
		WriteServiceError(w, service.ErrForbidden)
		return
	}

	workbook := excelize.NewFile()
	defer func() { _ = workbook.Close() }()

	sheetName := workbook.GetSheetName(workbook.GetActiveSheetIndex())
	_ = workbook.SetSheetName(sheetName, "Users")

	headers := []string{
		"email",
		"full_name",
		"dharma_name",
		"birth_year",
		"phone",
		"ctn",
		"password",
		"is_admin",
		"can_manage_publications",
		"is_active",
	}
	sampleRow := []string{
		"user@example.com",
		"Nguyen Van A",
		"Thich Tam Duc",
		"1995",
		"0901234567",
		"Ban Truyen Thong",
		"MatKhau123",
		"false",
		"true",
		"true",
	}

	for i, header := range headers {
		cell, err := excelize.CoordinatesToCellName(i+1, 1)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "template_error", err.Error())
			return
		}
		if err := workbook.SetCellValue("Users", cell, header); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "template_error", err.Error())
			return
		}
	}
	for i, value := range sampleRow {
		cell, err := excelize.CoordinatesToCellName(i+1, 2)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "template_error", err.Error())
			return
		}
		if err := workbook.SetCellValue("Users", cell, value); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "template_error", err.Error())
			return
		}
	}

	headerStyle, err := workbook.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"2563EB"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center"},
	})
	if err == nil {
		_ = workbook.SetCellStyle("Users", "A1", "J1", headerStyle)
	}
	_ = workbook.SetColWidth("Users", "A", "A", 30)
	_ = workbook.SetColWidth("Users", "B", "B", 24)
	_ = workbook.SetColWidth("Users", "C", "C", 24)
	_ = workbook.SetColWidth("Users", "D", "D", 14)
	_ = workbook.SetColWidth("Users", "E", "E", 18)
	_ = workbook.SetColWidth("Users", "F", "F", 24)
	_ = workbook.SetColWidth("Users", "G", "J", 24)
	_ = workbook.SetPanes("Users", &excelize.Panes{
		Freeze:      true,
		Split:       false,
		XSplit:      0,
		YSplit:      1,
		TopLeftCell: "A2",
		ActivePane:  "bottomLeft",
	})

	var buf bytes.Buffer
	if err := workbook.Write(&buf); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "template_error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="pqmedia-user-import-template.xlsx"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

func (h UserHandler) CommitImport(w http.ResponseWriter, r *http.Request) {
	actor := authctx.MustPrincipal(r.Context())
	var body commitUserImportRequest
	if err := httpx.ReadJSON(r, &body); err != nil {
		if errors.Is(err, io.EOF) {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_body", "missing body")
			return
		}
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}

	result, err := h.Service.CommitUserImport(r.Context(), actor, service.CommitUserImportInput{
		ImportID: body.ImportID,
	})
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, userImportCommitResponse{Summary: toUserImportSummaryDTO(result.Summary)})
}

func (h UserHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	actor := authctx.MustPrincipal(r.Context())
	userID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_user_id", err.Error())
		return
	}
	var body updateProfileRequest
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	updated, err := h.Service.UpdateProfile(r.Context(), actor, userID, service.UpdateProfileInput{
		FullName:   body.FullName,
		DharmaName: body.DharmaName,
		BirthYear:  body.BirthYear,
		Phone:      body.Phone,
		CTN:        body.CTN,
	})
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ToPrincipal(updated, nil))
}

func (h UserHandler) Update(w http.ResponseWriter, r *http.Request) {
	actor := authctx.MustPrincipal(r.Context())
	userID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_user_id", err.Error())
		return
	}
	var body updateUserRequest
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	updated, err := h.Service.UpdateUser(r.Context(), actor, userID, service.UpdateUserInput{
		FullName:              body.FullName,
		DharmaName:            body.DharmaName,
		BirthYear:             body.BirthYear,
		Phone:                 body.Phone,
		CTN:                   body.CTN,
		IsAdmin:               body.IsAdmin,
		CanManagePublications: body.CanManagePublications,
		IsActive:              body.IsActive,
	})
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ToPrincipal(updated, nil))
}

func (h UserHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	actor := authctx.MustPrincipal(r.Context())
	userID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_user_id", err.Error())
		return
	}
	var body resetUserPasswordRequest
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if err := h.Service.ResetUserPassword(r.Context(), actor, userID, service.ResetUserPasswordInput{
		CurrentPassword: body.CurrentPassword,
		Password:        body.Password,
	}); err != nil {
		WriteServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h UserHandler) UpdateOwnProfile(w http.ResponseWriter, r *http.Request) {
	actor := authctx.MustPrincipal(r.Context())
	var body updateProfileRequest
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	updated, err := h.Service.UpdateProfile(r.Context(), actor, actor.User.ID, service.UpdateProfileInput{
		FullName:   body.FullName,
		DharmaName: body.DharmaName,
		BirthYear:  body.BirthYear,
		Phone:      body.Phone,
		CTN:        body.CTN,
	})
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ToPrincipal(updated, nil))
}

func (h UserHandler) UpdateOwnPassword(w http.ResponseWriter, r *http.Request) {
	actor := authctx.MustPrincipal(r.Context())
	var body resetUserPasswordRequest
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if err := h.Service.ResetUserPassword(r.Context(), actor, actor.User.ID, service.ResetUserPasswordInput{
		CurrentPassword: body.CurrentPassword,
		Password:        body.Password,
	}); err != nil {
		WriteServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
