package service

import (
	"encoding/json"
	"flhansen/application-manager/login-service/src/auth"
	"flhansen/application-manager/login-service/src/database"
	"flhansen/application-manager/login-service/src/security"
	"fmt"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt"
	"github.com/julienschmidt/httprouter"
)

type JwtConfig struct {
	SignKey interface{}
}

type LoginService struct {
	Port       int
	Host       string
	Router     *httprouter.Router
	JwtSignKey interface{}
	Database   *database.PostgresContext
}

func (service *LoginService) LoginHandler(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	var req LoginRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, NewApiResponse(http.StatusInternalServerError, "An error occured while parsing the request body"))
		return
	}

	acc, err := service.Database.GetAccountByUsername(req.Username)

	if err != nil || !security.ValidatePassword(req.Password, acc.Password) || acc.Id == 0 {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, NewApiResponse(http.StatusUnauthorized, "Wrong credentials"))
		return
	}

	signedToken, err := auth.GenerateToken(acc.Id, acc.Username, jwt.SigningMethodHS256, service.JwtSignKey)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, NewApiResponse(http.StatusInternalServerError, "Could not create token"))
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, NewApiResponseObject(http.StatusOK, "User has been logged in", map[string]interface{}{"token": signedToken}))
}

func (service *LoginService) RegisterHandler(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	var req RegisterRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, NewApiResponse(http.StatusInternalServerError, "An error occured while parsing the request body"))
		return
	}

	_, err := service.Database.InsertAccount(req.Username, req.Password, req.Email, time.Now())

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, NewApiResponse(http.StatusBadRequest, "User already exists"))
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, NewApiResponse(http.StatusOK, "User registered"))
}

func (service *LoginService) DeleteHandler(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	username := r.Header.Get("username")

	if err := service.Database.DeleteAccountByUsername(username); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, NewApiResponse(http.StatusUnauthorized, "Error while trying to delete the user"))
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, NewApiResponse(http.StatusOK, "User deleted"))
}

func Authenticated(service LoginService, handler httprouter.Handle) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		tokenString := r.Header.Get("Authorization")
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}

			return service.JwtSignKey, nil
		})

		if err != nil {
			w.Header().Set("WWW-Authenticate", "Basic realm=Restricted")
			http.Error(w, NewApiResponse(http.StatusUnauthorized, "You are not allowed"), http.StatusUnauthorized)
			return
		}

		claims := token.Claims.(jwt.MapClaims)
		r.Header.Add("username", claims["username"].(string))
		handler(w, r, p)
	}
}

func New(config ServiceConfig) *LoginService {
	context := database.NewContext(
		config.Database.Host,
		config.Database.Port,
		config.Database.Username,
		config.Database.Password,
		config.Database.Database)

	service := LoginService{
		Port:       config.Port,
		Host:       config.Host,
		Router:     httprouter.New(),
		JwtSignKey: config.Jwt.SignKey,
		Database:   context,
	}

	service.Router.POST("/api/auth/login", service.LoginHandler)
	service.Router.POST("/api/auth/register", service.RegisterHandler)
	service.Router.DELETE("/api/auth/delete", Authenticated(service, service.DeleteHandler))

	return &service
}

func (service LoginService) Start() error {
	return http.ListenAndServe(fmt.Sprintf("%s:%d", service.Host, service.Port), service.Router)
}
