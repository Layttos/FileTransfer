package routes

import (
	"encoding/json"
	"filetransfer-backend/postsql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

func writeJSONMessage(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
}

var RenameBody struct {
}

type rqBody struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	Username   string `json:"username"`
	FirstName  string `json:"firstName"`
	LastName   string `json:"lastName"`
	InviteCode string `json:"inviteCode"`
	Token      string `json:"token"`
	Action     string `json:"action"`
	FileID     string `json:"fileID"`
	NewName    string `json:"newFileName"`
	NewFileID  string `json:"newFileID"`
}

func HandleAdminLogin(w http.ResponseWriter, req *http.Request) {

	admin_username, err := req.Cookie("admin_username")
	admin_password, err1 := req.Cookie("admin_password")
	if (err != nil && err1 == nil) || (err == nil && err1 != nil) {
		writeJSONMessage(w, "USER_OR_PASSWORD_INCORRECT")
	}

	if admin_username != nil && admin_password != nil {
		if postsql.AdminCheckCredentials(admin_username.Value, admin_password.Value) {
			writeJSONMessage(w, "USER_ALREADY_LOGGED_IN")
			http.ServeFile(w, req, "./public/admin/administration.html")
			return
		}
	}

	http.ServeFile(w, req, "./public/admin/login.html")
	return

}

func HandleAdminRegister(w http.ResponseWriter, req *http.Request) {
	http.ServeFile(w, req, "./public/admin/signin.html")
}

func HandleAdmin(w http.ResponseWriter, req *http.Request) {
	http.ServeFile(w, req, "./public/admin/administration.html")
	return
}

func HandleAdminAPI(w http.ResponseWriter, req *http.Request) {
	var payload rqBody

	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		writeJSONMessage(w, "The request body is not valid JSON")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	switch payload.Action {
	case "delete":
		{

			if !postsql.AdminCheckCredentials(payload.Username, payload.Password) {
				json.NewEncoder(w).Encode(`{"message": "The provided credentials are not valid"}`)
				return
			}

			if !postsql.Exists(payload.FileID) {
				json.NewEncoder(w).Encode(`{"message":"The ID you provided wasn't found'"}`)
				return

			}

			postsql.DeleteFile(payload.FileID)
			json.NewEncoder(w).Encode(`{"message": "Deleted file ID ''` + payload.FileID + `'"}`)
			return
		}

	case "rename":
		{
			if !postsql.AdminCheckCredentials(payload.Username, payload.Password) {
				json.NewEncoder(w).Encode(`{"message": "The provided credentials are not valid"}`)
				return
			}

			if !postsql.Exists(payload.FileID) {
				json.NewEncoder(w).Encode(`{"message":"The ID you provided wasn't found'"}`)
				return

			}

			fullPath := filepath.Join(os.Getenv("FILES_PATH"), payload.FileID, postsql.GetFileName(payload.FileID))
			fmt.Println("[RENAME] Full path:", fullPath)

			if _, err := os.Stat(fullPath); os.IsNotExist(err) {
				json.NewEncoder(w).Encode(`{"message": "Somehow the ID was found but not the file"}`)
				return
			}

			if postsql.RenameFile(payload.FileID, payload.NewName) {
				writeJSONMessage(w, "FILE_RENAMED_SUCCESSFULLY")
			} else {
				writeJSONMessage(w, "FAILED_TO_RENAME_FILE")
			}
			return

		}
	case "change_id":
		{
			if !postsql.AdminCheckCredentials(payload.Username, payload.Password) {
				json.NewEncoder(w).Encode(`{"message": "The provided credentials are not valid"}`)
				return
			}

			if !postsql.Exists(payload.FileID) {
				json.NewEncoder(w).Encode(`{"message":"The ID you provided wasn't found'"}`)
				return

			}

			fullPath := filepath.Join(os.Getenv("FILES_PATH"), payload.FileID, postsql.GetFileName(payload.FileID))

			if _, err := os.Stat(fullPath); os.IsNotExist(err) {
				json.NewEncoder(w).Encode(`{"message": "Somehow the ID was found but not the file"}`)
				return
			}

			if postsql.ChangeFileID(payload.FileID, payload.NewFileID) {
				writeJSONMessage(w, "FILE_ID_CHANGED_SUCCESSFULLY")
			} else {
				writeJSONMessage(w, "FAILED_TO_CHANGE_FILE_ID")
			}

			return
		}
	case "register":
		{
			if payload.Email == "" || payload.Password == "" || payload.Username == "" || payload.FirstName == "" || payload.LastName == "" || payload.InviteCode == "" {
				json.NewEncoder(w).Encode(`{"message": "ONE_OR_MORE_FIELDS_ARE_EMPTY"}`)
				return
			}

			if postsql.AdminCheckUserExistence(payload.Username) {
				writeJSONMessage(w, "USERNAME_OR_EMAIL_ALREADY_EXISTS")
				return
			}
			token, success := postsql.AdminRegisterUser(payload.FirstName, payload.LastName, payload.Username, payload.Email, payload.Password, payload.InviteCode)
			if !success {
				writeJSONMessage(w, "USER_CREATION_FAILED")
				return
			}
			fmt.Println("User created successfully")

			json.NewEncoder(w).Encode(`{"message": "USER_CREATED_SUCCESSFULLY", "token":"` + token + `"	}`)
			return

		}
	case "login":
		{
			if payload.Username == "" || payload.Password == "" || payload.Email == "" {
				json.NewEncoder(w).Encode(`{"message": "ONE_OR_MORE_FIELDS_ARE_EMPTY"}`)
				return
			}

			if !postsql.AdminCheckCredentials(payload.Username, payload.Password) {
				writeJSONMessage(w, "USER_OR_PASSWORD_INCORRECT")
				return
			}

			token := postsql.AdminGetUserToken(payload.Username)
			if token == "" {
				token = postsql.AdminGetUserToken(payload.Email)
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "USER_LOGGED_IN_SUCCESSFULLY", "token": token})
			return
		}
	case "list_files":
		{
			if !postsql.AdminCheckCredentials(payload.Username, payload.Password) {
				json.NewEncoder(w).Encode(`{"message": "The provided credentials are not valid"}`)
				return
			}

			if req.URL.Query().Get("offset") == "" || req.URL.Query().Get("limit") == "" {
				writeJSONMessage(w, "MISSING_OFFSET_OR_LIMIT")
				return
			}

			offset, err1 := strconv.Atoi(req.URL.Query().Get("offset"))
			limit, err2 := strconv.Atoi(req.URL.Query().Get("limit"))

			if err1 != nil || err2 != nil {
				writeJSONMessage(w, "INVALID_OFFSET_OR_LIMIT")
				return
			}

			files := postsql.ListFiles(offset, limit)
			json.NewEncoder(w).Encode(files)
			return
		}
	case "download":
		{
			if !postsql.AdminCheckCredentials(payload.Username, payload.Password) {
				json.NewEncoder(w).Encode(`{"message": "The provided credentials are not valid"}`)
				return
			}

			if !postsql.Exists(payload.FileID) {
				json.NewEncoder(w).Encode(`{"message":"The ID you provided wasn't found'"}`)
				return
			}

			fullPath := filepath.Join(os.Getenv("FILES_PATH"), payload.FileID, postsql.GetFileName(payload.FileID))

			if _, err := os.Stat(fullPath); os.IsNotExist(err) {
				json.NewEncoder(w).Encode(`{"message": "Somehow the ID was found but not the file"}`)
				return
			}

			w.Header().Set("Content-Disposition", "attachment; filename=\""+postsql.GetFileName(payload.FileID)+"\"")
			w.Header().Set("Content-Type", "application/octet-stream")
			http.ServeFile(w, req, fullPath)
			return

		}
	default:
		{
			writeJSONMessage(w, "NON_EXISTENT_ACTION")
		}
		return
	}

}
