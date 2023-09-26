package parsemail

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
)

const contentTypeMultipartMixed = "multipart/mixed"
const contentTypeMultipartAlternative = "multipart/alternative"
const contentTypeMultipartRelated = "multipart/related"
const contentTypeTextHtml = "text/html"
const contentTypeTextPlain = "text/plain"

func Parse(r io.Reader) (email Email, err error) {

	msg, err := mail.ReadMessage(r)
	if err != nil {
		return
	}

	email.ContentType = msg.Header.Get("Content-Type")
	contentType, params, err := parseContentType(email.ContentType)
	if err != nil {
		return
	}

	switch contentType {
	case contentTypeMultipartMixed:
		email.TextBody, email.HTMLBody, email.Attachments, email.EmbeddedFiles, err = parseMultipartMixed(msg.Body, params["boundary"])
	case contentTypeMultipartAlternative:
		email.TextBody, email.HTMLBody, email.EmbeddedFiles, err = parseMultipartAlternative(msg.Body, params["boundary"])
	case contentTypeMultipartRelated:
		email.TextBody, email.HTMLBody, email.EmbeddedFiles, err = parseMultipartRelated(msg.Body, params["boundary"])
	case contentTypeTextPlain:
		message, _ := io.ReadAll(msg.Body)
		email.TextBody = strings.TrimSuffix(string(message[:]), "\n")
	case contentTypeTextHtml:
		message, _ := io.ReadAll(msg.Body)
		email.HTMLBody = strings.TrimSuffix(string(message[:]), "\n")
	default:
		email.Content, err = decodeContent(msg.Body, msg.Header.Get("Content-Transfer-Encoding"))
	}

	return
}

func parseContentType(contentTypeHeader string) (contentType string, params map[string]string, err error) {
	if contentTypeHeader == "" {
		contentType = contentTypeTextPlain
		return
	}

	return mime.ParseMediaType(contentTypeHeader)
}

func parseMultipartRelated(msg io.Reader, boundary string) (textBody, htmlBody string, embeddedFiles []EmbeddedFile, err error) {
	pmr := multipart.NewReader(msg, boundary)
	for {
		part, pmrErr := pmr.NextPart()

		if pmrErr == io.EOF {
			break
		} else if pmrErr != nil {
			err = pmrErr
			return
		}

		contentType, params, mimeErr := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if mimeErr != nil {
			err = pmrErr
			return
		}

		switch contentType {
		case contentTypeTextPlain:
			ppContent, ioErr := io.ReadAll(part)
			if ioErr != nil {
				err = ioErr
				return
			}

			textBody += strings.TrimSuffix(string(ppContent[:]), "\n")
		case contentTypeTextHtml:
			ppContent, ioErr := io.ReadAll(part)
			if ioErr != nil {
				err = ioErr
				return
			}

			htmlBody += strings.TrimSuffix(string(ppContent[:]), "\n")
		case contentTypeMultipartAlternative:
			tb, hb, ef, mpaErr := parseMultipartAlternative(part, params["boundary"])
			if mpaErr != nil {
				err = mpaErr
				return
			}

			htmlBody += hb
			textBody += tb
			embeddedFiles = append(embeddedFiles, ef...)
		default:
			ef, efErr := decodeEmbeddedFile(part)
			if efErr != nil {
				err = efErr
				return
			}

			embeddedFiles = append(embeddedFiles, ef)
		}
	}

	return
}

func parseMultipartAlternative(msg io.Reader, boundary string) (textBody, htmlBody string, embeddedFiles []EmbeddedFile, err error) {
	pmr := multipart.NewReader(msg, boundary)
	for {
		part, pmrErr := pmr.NextPart()

		if pmrErr == io.EOF {
			break
		} else if pmrErr != nil {
			err = pmrErr
			return
		}

		contentType, params, mimeErr := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if mimeErr != nil {
			err = mimeErr
			return
		}

		switch contentType {
		case contentTypeTextPlain:
			ppContent, ioErr := io.ReadAll(part)
			if ioErr != nil {
				err = ioErr
				return
			}

			textBody += strings.TrimSuffix(string(ppContent[:]), "\n")
		case contentTypeTextHtml:
			ppContent, ioErr := io.ReadAll(part)
			if ioErr != nil {
				err = ioErr
				return
			}

			htmlBody += strings.TrimSuffix(string(ppContent[:]), "\n")
		case contentTypeMultipartRelated:
			tb, hb, ef, mprErr := parseMultipartRelated(part, params["boundary"])
			if mprErr != nil {
				err = mprErr
				return
			}

			htmlBody += hb
			textBody += tb
			embeddedFiles = append(embeddedFiles, ef...)
		default:
			ef, efErr := decodeEmbeddedFile(part)
			if efErr != nil {
				err = efErr
				return
			}

			embeddedFiles = append(embeddedFiles, ef)
		}
	}

	return
}

func parseMultipartMixed(msg io.Reader, boundary string) (textBody, htmlBody string, attachments []Attachment, embeddedFiles []EmbeddedFile, err error) {
	pmr := multipart.NewReader(msg, boundary)
	for {
		part, pmrErr := pmr.NextPart()
		if pmrErr == io.EOF {
			break
		} else if pmrErr != nil {
			err = pmrErr
			return
		}

		contentType, params, mimeErr := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if mimeErr != nil {
			err = mimeErr
			return
		}

		switch contentType {
		case contentTypeMultipartAlternative:
			textBody, htmlBody, embeddedFiles, err = parseMultipartAlternative(part, params["boundary"])
			if err != nil {
				return
			}

		case contentTypeMultipartRelated:
			textBody, htmlBody, embeddedFiles, err = parseMultipartRelated(part, params["boundary"])
			if err != nil {
				return
			}

		default:
			if isAttachment(part) {
				at, aErr := decodeAttachment(part)
				if aErr != nil {
					err = aErr
					return
				}

				attachments = append(attachments, at)
			} else {
				ppContent, ioErr := io.ReadAll(part)
				if ioErr != nil {
					err = ioErr
					return
				}

				switch contentType {
				case contentTypeTextPlain:
					textBody += strings.TrimSuffix(string(ppContent[:]), "\n")
				case contentTypeTextHtml:
					htmlBody += strings.TrimSuffix(string(ppContent[:]), "\n")
				}
			}
		}
	}

	return
}

func decodeMimeSentence(s string) string {
	result := []string{}
	ss := strings.Split(s, " ")

	for _, word := range ss {
		dec := new(mime.WordDecoder)
		w, err := dec.Decode(word)
		if err != nil {
			if len(result) == 0 {
				w = word
			} else {
				w = " " + word
			}
		}

		result = append(result, w)
	}

	return strings.Join(result, "")
}

func decodeEmbeddedFile(part *multipart.Part) (ef EmbeddedFile, err error) {
	cid := decodeMimeSentence(part.Header.Get("Content-Id"))
	decoded, err := decodeContent(part, part.Header.Get("Content-Transfer-Encoding"))
	if err != nil {
		return
	}

	_, params, _ := parseContentType(part.Header.Get("Content-Type"))
	if name, ok := params["name"]; ok {
		ef.Filename = name
	}

	ef.CID = strings.Trim(cid, "<>")
	ef.Data = decoded
	ef.ContentType = part.Header.Get("Content-Type")

	return
}

func isAttachment(part *multipart.Part) bool {
	return part.FileName() != ""
}

func decodeAttachment(part *multipart.Part) (at Attachment, err error) {
	filename := decodeMimeSentence(part.FileName())
	decoded, err := decodeContent(part, part.Header.Get("Content-Transfer-Encoding"))
	if err != nil {
		return
	}

	at.Filename = filename
	at.Data = decoded
	at.ContentType = strings.Split(part.Header.Get("Content-Type"), ";")[0]

	return
}

func decodeContent(content io.Reader, encoding string) (io.Reader, error) {
	switch encoding {
	case "base64":
		decoded := base64.NewDecoder(base64.StdEncoding, content)
		b, err := io.ReadAll(decoded)
		if err != nil {
			return nil, err
		}

		return bytes.NewReader(b), nil
	case "8bit", "7bit":
		dd, err := io.ReadAll(content)
		if err != nil {
			return nil, err
		}

		return bytes.NewReader(dd), nil
	case "":
		return content, nil
	default:
		return nil, fmt.Errorf("unknown encoding: %s", encoding)
	}
}

// Attachment with filename, content type and data (as a io.Reader)
type Attachment struct {
	Filename    string
	ContentType string
	Data        io.Reader
}

// EmbeddedFile with content id, content type and data (as a io.Reader)
type EmbeddedFile struct {
	CID         string
	Filename    string
	ContentType string
	Data        io.Reader
}

// Email with fields for all the headers defined in RFC5322 with it's attachments and
type Email struct {
	Header mail.Header

	ContentType string
	Content     io.Reader

	HTMLBody string
	TextBody string

	Attachments   []Attachment
	EmbeddedFiles []EmbeddedFile
}
