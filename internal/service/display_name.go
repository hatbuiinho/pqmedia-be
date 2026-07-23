package service

import "pqmedia/be/internal/repository"

func preferredProfileName(profile repository.Profile) string {
	if profile.DharmaName != nil && *profile.DharmaName != "" {
		return *profile.DharmaName
	}
	return profile.FullName
}
