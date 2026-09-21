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

func admitProjectDatabase(w http.ResponseWriter, r *http.Request, path string) bool {
	db, err := utils.OpenDb(path)
	if err != nil {
		http.Error(w, "Project unavailable", http.StatusServiceUnavailable)
		return false
	}
	defer db.Close()
	schema, err := compatibility.ReadSchema(db)
	if err != nil {
		http.Error(w, "Project unavailable during maintenance", http.StatusServiceUnavailable)
		return false
	}
	compatibility.Respond(w, schema)
	if err := compatibility.Admit(r.Header, schema); err != nil {
		compatibility.WriteError(w, err)
		return false
	}
	return true
}

func projectAdmission(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := getAuthUser(r)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		path, err := safeProjectPath(CONFIG.ProjectsDir, r.PathValue("project"))
		if err != nil {
			http.Error(w, "Invalid project name", http.StatusBadRequest)
			return
		}
		isRoot := r.URL.Path == "/"+r.PathValue("project")
		if isRoot && r.Method == http.MethodPost {
			if err := compatibility.Admit(r.Header, compatibility.Schema); err != nil {
				compatibility.WriteError(w, err)
				return
			}
			compatibility.Respond(w, compatibility.Schema)
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
		if !(isRoot && r.Method == http.MethodGet) && !admitProjectDatabase(w, r, path) {
			return
		}
		next(w, r)
	}
}
