package handlers

import (
	"net/http"

	"pqmedia/be/internal/authctx"
	"pqmedia/be/internal/httpx"
	"pqmedia/be/internal/storage"
)

type UploadHandler struct {
	Storage *storage.MinIO
}

type presignRequest struct {
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	Kind        string `json:"kind"` // "image" | "video" | "avatar"
}

func (h UploadHandler) Presign(w http.ResponseWriter, r *http.Request) {
	principal := authctx.MustPrincipal(r.Context())
	var body presignRequest
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if body.FileName == "" {
		httpx.WriteError(w, http.StatusBadRequest, "validation_failed", "file_name is required")
		return
	}
	prefix := uploadPrefix(body.Kind)
	if prefix == "" {
		httpx.WriteError(w, http.StatusBadRequest, "validation_failed", "kind must be image|video|avatar")
		return
	}
	out, err := h.Storage.PresignUpload(r.Context(), prefix, principal.User.ID.String(), body.FileName)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "presign_failed", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func uploadPrefix(kind string) string {
	switch kind {
	case "image", "video":
		return "posts"
	case "avatar":
		return "avatars"
	default:
		return ""
	}
}
