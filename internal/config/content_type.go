package config

import (
	"fmt"
	"log/slog"
)

type ContentType string

const (
	ContentTypeUndefined ContentType = "undefined"
	ContentTypePDF       ContentType = "application/pdf"
	ContentTypeHTML      ContentType = "text/html"
	ContentTypeEmail     ContentType = "message/rfc822"
)

func (c ContentType) FileType() (string, error) {
	switch c {
	case ContentTypePDF:
		return "pdf", nil
	case ContentTypeHTML:
		return "html", nil
	case ContentTypeEmail:
		return "eml", nil
	default:
		return "", fmt.Errorf("could not convert content type '%v' to a file type", string(c))
	}
}

func (c ContentType) LogValue() slog.Value {
	return slog.StringValue(string(c))
}
