package dto

// BrandingResponse is the body for GET /branding (public) and reflects the
// current instance-wide logo, favicon, brand name, and primary colors. URL
// fields are presigned GET URLs resolved from the stored object-storage
// keys, nil when that slot has no image uploaded.
type BrandingResponse struct {
	LogoURL           *string `json:"logo_url,omitempty"`
	LogoThumbURL      *string `json:"logo_thumb_url,omitempty"`
	FaviconURL        *string `json:"favicon_url,omitempty"`
	FaviconThumbURL   *string `json:"favicon_thumb_url,omitempty"`
	BrandName         *string `json:"brand_name,omitempty"`
	PrimaryColorLight *string `json:"primary_color_light,omitempty"`
	PrimaryColorDark  *string `json:"primary_color_dark,omitempty"`
}

// UpdateSettingsRequest is the body for PATCH /admin/settings. A nil or
// empty value clears that field's override.
type UpdateSettingsRequest struct {
	BrandName         *string `json:"brand_name"`
	PrimaryColorLight *string `json:"primary_color_light"`
	PrimaryColorDark  *string `json:"primary_color_dark"`
}

// AvatarShapedImageResponse is the response body for the logo/favicon
// initiate/complete/delete endpoints. It's deliberately shaped as
// {avatar_url, avatar_thumb_url} — the same generic shape every other
// avatar-bearing resource's complete/delete endpoint embeds — rather than
// {logo_url, ...}/{favicon_url, ...}, so the frontend can drive uploads for
// both slots through its existing generic avatar-upload client and
// <AvatarUpload> component unchanged. BrandingResponse (above) is the
// clearer shape used everywhere the app actually consumes branding.
type AvatarShapedImageResponse struct {
	AvatarURL      *string `json:"avatar_url,omitempty"`
	AvatarThumbURL *string `json:"avatar_thumb_url,omitempty"`
}

// EmailSettingsResponse is the body for GET /admin/settings/email. It
// never includes the SMTP password — only PasswordSet reports whether one is
// stored.
type EmailSettingsResponse struct {
	FromEmail            string `json:"from_email"`
	FromName             string `json:"from_name"`
	Host                 string `json:"host"`
	Port                 int    `json:"port"`
	Username             string `json:"username"`
	UseSSL               bool   `json:"use_ssl"`
	UseTLS               bool   `json:"use_tls"`
	SkipVerify           bool   `json:"skip_verify"`
	SendUserCreatedEmail bool   `json:"send_user_created_email"`
	PasswordSet          bool   `json:"password_set"`
	Configured           bool   `json:"configured"`
}

// UpdateEmailSettingsRequest is the body for PATCH /admin/settings/email.
// Password is tri-state: omit/null keeps the stored password, "" clears it,
// any other value replaces it.
type UpdateEmailSettingsRequest struct {
	FromEmail            string  `json:"from_email"`
	FromName             string  `json:"from_name"`
	Host                 string  `json:"host"`
	Port                 int     `json:"port"`
	Username             string  `json:"username"`
	Password             *string `json:"password"`
	UseSSL               bool    `json:"use_ssl"`
	UseTLS               bool    `json:"use_tls"`
	SkipVerify           bool    `json:"skip_verify"`
	SendUserCreatedEmail bool    `json:"send_user_created_email"`
}

// SendTestEmailRequest is the body for POST /admin/settings/email/test.
type SendTestEmailRequest struct {
	To string `json:"to" binding:"required,email"`
}
