package review

import (
	"bytes"
	"embed"
	"errors"
	"html/template"
	"log/slog"
	"strconv"

	"github.com/gofiber/fiber/v2"
)

//go:embed templates/review.html
var templateFS embed.FS

// Handler serves the operator review queue UI over HTTP using Fiber.
// It renders a plain HTML page listing flagged price discrepancies and
// exposes POST endpoints to approve or reject individual entries.
type Handler struct {
	store Store
	tmpl  *template.Template
}

// templateFuncs provides helper functions available inside the review template.
var templateFuncs = template.FuncMap{
	// mulf multiplies two float64 values, used to convert decimal delta to percentage.
	"mulf": func(a, b float64) float64 { return a * b },
}

// NewHandler returns a Handler wired with the given Store. The HTML template
// is parsed once at construction; any parse error is fatal (indicates a
// programmer error, not a runtime condition).
func NewHandler(store Store) *Handler {
	tmpl := template.Must(
		template.New("review.html").Funcs(templateFuncs).ParseFS(templateFS, "templates/review.html"),
	)
	return &Handler{store: store, tmpl: tmpl}
}

// List handles GET /admin/review.
// It fetches all pending review_queue rows and renders them as an HTML table.
// The template is rendered into a buffer first so that a render error can
// still return a proper 500 status rather than appending an error message to
// a partially-written 200 response.
func (h *Handler) List(c *fiber.Ctx) error {
	items, err := h.store.ListPending(c.Context())
	if err != nil {
		slog.Error("review handler: list pending", "err", err)
		return c.Status(fiber.StatusInternalServerError).SendString("internal server error")
	}

	var buf bytes.Buffer
	if err := h.tmpl.Execute(&buf, items); err != nil {
		slog.Error("review handler: render template", "err", err)
		return c.Status(fiber.StatusInternalServerError).SendString("internal server error")
	}

	c.Set(fiber.HeaderContentType, "text/html; charset=utf-8")
	return c.Send(buf.Bytes())
}

// Approve handles POST /admin/review/:id/approve.
// It validates the id path parameter, calls Store.Approve, then redirects
// back to the review list. Returns 409 Conflict when the entry no longer
// exists or has already been actioned.
func (h *Handler) Approve(c *fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil || id <= 0 {
		slog.Warn("review handler: approve: invalid id", "raw", c.Params("id"))
		return c.Status(fiber.StatusBadRequest).SendString("invalid id: must be a positive integer")
	}

	if err := h.store.Approve(c.Context(), id); err != nil {
		if errors.Is(err, ErrNotPending) {
			return c.Status(fiber.StatusConflict).SendString("entry not found or already resolved")
		}
		slog.Error("review handler: approve", "id", id, "err", err)
		return c.Status(fiber.StatusInternalServerError).SendString("internal server error")
	}

	return c.Redirect("/admin/review", fiber.StatusSeeOther)
}

// Reject handles POST /admin/review/:id/reject.
// It validates the id path parameter, calls Store.Reject, then redirects
// back to the review list. Returns 409 Conflict when the entry no longer
// exists or has already been actioned.
func (h *Handler) Reject(c *fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil || id <= 0 {
		slog.Warn("review handler: reject: invalid id", "raw", c.Params("id"))
		return c.Status(fiber.StatusBadRequest).SendString("invalid id: must be a positive integer")
	}

	if err := h.store.Reject(c.Context(), id); err != nil {
		if errors.Is(err, ErrNotPending) {
			return c.Status(fiber.StatusConflict).SendString("entry not found or already resolved")
		}
		slog.Error("review handler: reject", "id", id, "err", err)
		return c.Status(fiber.StatusInternalServerError).SendString("internal server error")
	}

	return c.Redirect("/admin/review", fiber.StatusSeeOther)
}
