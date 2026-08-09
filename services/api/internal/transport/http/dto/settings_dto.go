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
