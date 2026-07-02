package authorization

import "pqmedia/be/internal/repository"

func CanManagePublications(user repository.User) bool {
	return user.IsAdmin || user.CanManagePublications
}
