package authorization

import "pqmedia/be/internal/repository"

func CanReviewPosts(user repository.User) bool {
	return user.IsAdmin || user.CanReviewPosts
}
