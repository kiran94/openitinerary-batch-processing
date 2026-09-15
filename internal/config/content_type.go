package config

import "fmt"

type ContentType string

const (
	ContentTypePDF   ContentType = "application/pdf"
	ContentTypeHTML  ContentType = "text/html"
	ContentTypeEmail ContentType = "message/rfc822"
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
