package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"pqmedia/be/internal/authctx"
	"pqmedia/be/internal/httpx"
	"pqmedia/be/internal/repository"
	"pqmedia/be/internal/service"
)

type PostHandler struct {
	Service *service.PostService
}

type attachmentInput struct {
	Kind        string `json:"kind"`
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	Bucket      string `json:"bucket"`
	ObjectKey   string `json:"object_key"`
	SizeBytes   int64  `json:"size_bytes"`
	Width       *int   `json:"width,omitempty"`
	Height      *int   `json:"height,omitempty"`
	DurationMs  *int   `json:"duration_ms,omitempty"`
	SortOrder   int    `json:"sort_order,omitempty"`
}

func (a attachmentInput) toRepo() repository.PostAttachmentInput {
	return repository.PostAttachmentInput{
		Kind:        repository.AttachmentKind(a.Kind),
		FileName:    a.FileName,
		ContentType: a.ContentType,
		Bucket:      a.Bucket,
		ObjectKey:   a.ObjectKey,
		SizeBytes:   a.SizeBytes,
		Width:       a.Width,
		Height:      a.Height,
		DurationMs:  a.DurationMs,
		SortOrder:   a.SortOrder,
	}
}

type createPostRequest struct {
	Content     string            `json:"content"`
	Attachments []attachmentInput `json:"attachments"`
}

type updatePostRequest struct {
	Content     *string            `json:"content,omitempty"`
	Attachments *[]attachmentInput `json:"attachments,omitempty"`
}

type postFeedResponse struct {
	Items []PostDTO   `json:"items"`
	Page  PageMetaDTO `json:"page"`
}

func (h PostHandler) ListFeed(w http.ResponseWriter, r *http.Request) {
	viewer := authctx.MustPrincipal(r.Context())
	filter := repository.FeedFilter{
		Search: r.URL.Query().Get("q"),
	}
	if raw := r.URL.Query().Get("author"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_author", err.Error())
			return
		}
		filter.AuthorUserID = &id
	}
	if raw := r.URL.Query().Get("unpublished_on"); raw != "" {
		filter.UnpublishedOn = splitCSV(raw)
	}
	filter.Limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	filter.Offset, _ = strconv.Atoi(r.URL.Query().Get("offset"))

	posts, page, err := h.Service.ListFeed(r.Context(), viewer, filter)
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	items := make([]PostDTO, len(posts))
	for i, p := range posts {
		items[i] = ToPost(p)
	}
	httpx.WriteJSON(w, http.StatusOK, postFeedResponse{Items: items, Page: ToPageMeta(page)})
}

func (h PostHandler) Create(w http.ResponseWriter, r *http.Request) {
	viewer := authctx.MustPrincipal(r.Context())
	var body createPostRequest
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	post, err := h.Service.Create(r.Context(), viewer, service.CreatePostInput{
		Content:     body.Content,
		Attachments: toAttachmentInputs(body.Attachments),
	})
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, ToPost(post))
}

func (h PostHandler) Get(w http.ResponseWriter, r *http.Request) {
	viewer := authctx.MustPrincipal(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "postID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_post_id", err.Error())
		return
	}
	post, err := h.Service.GetPost(r.Context(), viewer, id)
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ToPost(post))
}

func (h PostHandler) Update(w http.ResponseWriter, r *http.Request) {
	viewer := authctx.MustPrincipal(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "postID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_post_id", err.Error())
		return
	}
	var body updatePostRequest
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	input := service.UpdatePostInput{Content: body.Content}
	if body.Attachments != nil {
		inputs := toAttachmentInputs(*body.Attachments)
		input.Attachments = &inputs
	}
	post, err := h.Service.Update(r.Context(), viewer, id, input)
	if err != nil {
		WriteServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ToPost(post))
}

func (h PostHandler) Delete(w http.ResponseWriter, r *http.Request) {
	viewer := authctx.MustPrincipal(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "postID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_post_id", err.Error())
		return
	}
	if err := h.Service.Delete(r.Context(), viewer, id); err != nil {
		WriteServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toAttachmentInputs(in []attachmentInput) []repository.PostAttachmentInput {
	out := make([]repository.PostAttachmentInput, len(in))
	for i, a := range in {
		out[i] = a.toRepo()
	}
	return out
}

func splitCSV(raw string) []string {
	out := make([]string, 0, 4)
	for _, p := range splitNonEmpty(raw, ',') {
		out = append(out, p)
	}
	return out
}

func splitNonEmpty(s string, sep rune) []string {
	out := []string{}
	cur := ""
	for _, r := range s {
		if r == sep {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
