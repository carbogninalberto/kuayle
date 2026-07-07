package dto

type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=12"`
}

type RegisterRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=12,max=128"`
	Name     string `json:"name" validate:"required,min=1,max=100"`
}

type TokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

type UserResponse struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	Name        string  `json:"name"`
	DisplayName string  `json:"display_name"`
	AvatarURL   *string `json:"avatar_url"`
}

type UpdateProfileRequest struct {
	Name        *string        `json:"name" validate:"omitempty,min=1,max=100"`
	DisplayName *string        `json:"display_name" validate:"omitempty,max=100"`
	AvatarURL   OptionalString `json:"avatar_url"`
}
