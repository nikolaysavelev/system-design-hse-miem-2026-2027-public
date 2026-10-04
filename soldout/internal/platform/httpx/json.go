// Package httpx — общие примитивы HTTP-слоя: JSON, ошибки, middleware, health.
package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

const maxBodyBytes = 1 << 20

// JSON пишет ответ с кодом status.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

// Decode читает JSON-тело в dst; тело ограничено 1 МиБ, неизвестные поля запрещены.
func Decode(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return NewError(http.StatusBadRequest, "bad_request", fmt.Sprintf("некорректный JSON: %v", err))
	}
	if dec.More() {
		return NewError(http.StatusBadRequest, "bad_request", "ожидался один JSON-объект")
	}
	return nil
}

// Error — ошибка с HTTP-статусом и машиночитаемым кодом.
type Error struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
	cause   error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.cause)
	}
	return e.Code + ": " + e.Message
}

// Unwrap позволяет errors.Is/As добираться до причины.
func (e *Error) Unwrap() error { return e.cause }

// NewError создаёт ошибку HTTP.
func NewError(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// Wrap оборачивает причину в HTTP-ошибку.
func Wrap(status int, code string, cause error) *Error {
	return &Error{Status: status, Code: code, Message: cause.Error(), cause: cause}
}

// WriteError отдаёт ошибку клиенту: *Error — как есть, остальное — 500 без деталей.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var he *Error
	if errors.As(err, &he) {
		JSON(w, he.Status, map[string]any{"error": he})
		return
	}
	logFromContext(r.Context()).ErrorContext(r.Context(), "необработанная ошибка", "err", err, "path", r.URL.Path)
	JSON(w, http.StatusInternalServerError, map[string]any{"error": &Error{Code: "internal", Message: "внутренняя ошибка"}})
}
