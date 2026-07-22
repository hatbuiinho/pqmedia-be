package handlers

import (
	"pqmedia/be/internal/repository"
	"pqmedia/be/internal/storage"
)

func avatarURLForProfile(store *storage.MinIO, profile repository.Profile) *string {
	if store == nil || profile.AvatarObjectKey == nil || *profile.AvatarObjectKey == "" {
		return nil
	}
	url := store.BuildPublicURL(*profile.AvatarObjectKey)
	return &url
}
