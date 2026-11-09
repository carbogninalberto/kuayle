package domain

import (
	"regexp"
	"strings"
)

// TeamIconImagePrefix marks a team icon that is an uploaded image. The value is
// "image:<uuid>.<ext>", which fits the icon column. The team APIs never take it
// from a client-supplied icon string; only the icon upload sets it. A workspace
// import clears it, because the file is not part of the archive.
const TeamIconImagePrefix = "image:"

// TeamIconMaxBytes is the largest accepted team icon upload.
const TeamIconMaxBytes = 1024 * 1024

// TeamIconImageTypes maps the formats accepted for team icons to their file
// extension. Favicons are often .ico. SVG is excluded because it can carry script.
var TeamIconImageTypes = map[string]string{
	"image/png":                ".png",
	"image/jpeg":               ".jpg",
	"image/webp":               ".webp",
	"image/gif":                ".gif",
	"image/vnd.microsoft.icon": ".ico",
	"image/x-icon":             ".ico",
}

// TeamIconFilePattern matches a stored team icon file name.
var TeamIconFilePattern = regexp.MustCompile(`^[0-9a-f-]{36}\.(png|jpg|webp|gif|ico)$`)

// TeamIconContentType returns the content type to serve a stored icon file with.
func TeamIconContentType(file string) string {
	switch {
	case strings.HasSuffix(file, ".jpg"):
		return "image/jpeg"
	case strings.HasSuffix(file, ".webp"):
		return "image/webp"
	case strings.HasSuffix(file, ".gif"):
		return "image/gif"
	case strings.HasSuffix(file, ".ico"):
		return "image/x-icon"
	default:
		return "image/png"
	}
}

// TeamIconImageFile returns the stored file name of an image icon value.
func TeamIconImageFile(icon *string) (string, bool) {
	if icon == nil || !strings.HasPrefix(*icon, TeamIconImagePrefix) {
		return "", false
	}
	file := strings.TrimPrefix(*icon, TeamIconImagePrefix)
	return file, TeamIconFilePattern.MatchString(file)
}
