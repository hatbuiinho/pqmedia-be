package handlers

import (
	"net/http"
	"strconv"
	"time"

	"pqmedia/be/internal/authctx"
	"pqmedia/be/internal/httpx"
	"pqmedia/be/internal/service"
	"pqmedia/be/internal/storage"
)

type StatsHandler struct {
	Service *service.StatsService
	Storage *storage.MinIO
}

type memberActivityResponse struct {
	Items []MemberActivityRowDTO `json:"items"`
	Page  PageMetaDTO            `json:"page"`
}

func (h StatsHandler) GetPostOverview(w http.ResponseWriter, r *http.Request) {
	actor := authctx.MustPrincipal(r.Context())
	statsRange, err := parseStatsRangeQuery(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_range", err.Error())
		return
	}
	stats, err := h.Service.GetPostOverview(r.Context(), actor, statsRange)
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toPostOverviewStatsDTO(stats))
}

func (h StatsHandler) ListMemberActivity(w http.ResponseWriter, r *http.Request) {
	actor := authctx.MustPrincipal(r.Context())
	statsRange, err := parseStatsRangeQuery(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_range", err.Error())
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	rows, page, err := h.Service.ListMemberActivity(r.Context(), actor, service.MemberActivityFilter{
		Range:   statsRange,
		SortBy:  service.MemberActivitySortBy(r.URL.Query().Get("sort_by")),
		SortDir: service.SortDirection(r.URL.Query().Get("sort_dir")),
		Limit:   limit,
		Offset:  offset,
	})
	if err != nil {
		WriteServiceError(w, err)
		return
	}

	items := make([]MemberActivityRowDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, toMemberActivityRowDTO(row, avatarURLForProfile(h.Storage, row.Principal.Profile)))
	}
	httpx.WriteJSON(w, http.StatusOK, memberActivityResponse{
		Items: items,
		Page:  ToPageMeta(page),
	})
}

func parseStatsRangeQuery(r *http.Request) (service.StatsRange, error) {
	from, err := parseOptionalRFC3339(r.URL.Query().Get("from"))
	if err != nil {
		return service.StatsRange{}, err
	}
	to, err := parseOptionalRFC3339(r.URL.Query().Get("to"))
	if err != nil {
		return service.StatsRange{}, err
	}
	return service.StatsRange{From: from, To: to}, nil
}

func parseOptionalRFC3339(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
