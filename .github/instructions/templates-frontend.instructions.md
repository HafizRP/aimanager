---
description: "Use when creating or editing web templates, HTML layouts, or static CSS/JS in web/. Enforces Go template rendering conventions, security headers, and responsive Tailwind-style UI design."
applyTo: "web/**"
---

# Frontend & Templates Guidelines

## 1. Go HTML Template System
- Templates reside in `web/templates/` and are embedded via `embed.FS` in `web/web.go`.
- Pages inherit from `base.html` using Go template definitions:
  ```html
  {{define "content"}}
  ...
  {{end}}
  ```
- Render through `h.render(w, r, "page.html", "base.html", dataMap)`.
- Standard keys passed in data maps:
  - `ActivePage`: string identifier for sidebar navigation highlight (e.g. `"dashboard"`, `"keys"`, `"logs"`).
  - `CurrentUser`: `*models.User` object from request context.
  - `SuccessMsg` and `ErrorMsg`: feedback messages from query parameters.

## 2. Static Assets
- Static assets are located in `web/static/` and served at `/static/*`.
- Use relative paths: `/static/css/custom.css`, `/static/js/app.js`.

## 3. Security & Content Security Policy (CSP)
- CSP is enforced in middleware in `cmd/gateway/main.go`.
- Allowed external script/style domains include `cdn.jsdelivr.net`, `fonts.googleapis.com`, `fonts.gstatic.com`, and `app.midtrans.com`.
- Avoid adding third-party scripts from unapproved domains without updating the CSP header in `cmd/gateway/main.go`.
- Escape untrusted user input; Go's `html/template` auto-escapes HTML variables by default.
