package main

import (
	"clustta/internal/compatibility"
	"clustta/internal/repository"
	"clustta/internal/utils"
	"net/http"
	"strings"
)

type projectMux struct{ *http.ServeMux }

func (m *projectMux) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	if strings.Contains(pattern, "{project}") {
		m.ServeMux.HandleFunc(pattern, projectAdmission(handler))
		return
	}
	m.ServeMux.HandleFunc(pattern, handler)
}

func negotiateRequestAPI(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	api, err := compatibility.Negotiate(r.Header)
	if err != nil {
		compatibility.WriteError(w, err)
		return r, false
	}
	compatibility.Respond(w, api)
	return r.WithContext(compatibility.WithAPIContext(r.Context(), api)), true
}

func projectAdmission(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := getAuthUser(r)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		r, ok = negotiateRequestAPI(w, r)
		if !ok {
			return
		}
		path, err := safeProjectPath(CONFIG.ProjectsDir, r.PathValue("project"))
		if err != nil {
			http.Error(w, "Invalid project name", http.StatusBadRequest)
			return
		}
		isRoot := r.URL.Path == "/"+r.PathValue("project")
		if isRoot && r.Method == http.MethodPost {
			next(w, r)
			return
		}
		if !utils.FileExists(path) {
			http.Error(w, "Project not found", http.StatusNotFound)
			return
		}
		member, err := repository.UserInProject(path, user.Id)
		if err != nil {
			http.Error(w, "Project unavailable", http.StatusServiceUnavailable)
			return
		}
		if !member {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}
