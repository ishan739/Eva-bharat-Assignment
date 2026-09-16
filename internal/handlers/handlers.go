package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"ticket-system/internal/auth"
	"ticket-system/internal/middleware"
	"ticket-system/internal/store"
)

type API struct {
	Store  *store.Store
	Tokens *auth.TokenIssuer
}

func NewAPI(s *store.Store, tokens *auth.TokenIssuer) *API {
	return &API{Store: s, Tokens: tokens}
}

// ---------- shared helpers ----------

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func decodeJSON(r *http.Request, dst interface{}) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// ---------- health ----------

func (a *API) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---------- auth ----------

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type registerResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

func (a *API) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password are required")
		return
	}
	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to process password")
		return
	}

	user := store.User{
		ID:           uuid.NewString(),
		Email:        req.Email,
		PasswordHash: hash,
		CreatedAt:    time.Now().UTC(),
	}

	if err := a.Store.CreateUser(user); err != nil {
		if err == store.ErrDuplicateEmail {
			writeError(w, http.StatusConflict, "email already registered")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to register user")
		return
	}

	writeJSON(w, http.StatusCreated, registerResponse{ID: user.ID, Email: user.Email})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token string `json:"token"`
}

func (a *API) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))

	user, err := a.Store.GetUserByEmail(req.Email)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	if !auth.CheckPassword(user.PasswordHash, req.Password) {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	token, err := a.Tokens.Generate(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	writeJSON(w, http.StatusOK, loginResponse{Token: token})
}

// ---------- tickets ----------

const (
	StatusOpen       = "open"
	StatusInProgress = "in_progress"
	StatusClosed     = "closed"
)

// allowed forward transitions per the required status flow: open -> in_progress -> closed
var allowedTransitions = map[string]string{
	StatusOpen:       StatusInProgress,
	StatusInProgress: StatusClosed,
}

type ticketResponse struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func toTicketResponse(t *store.Ticket) ticketResponse {
	return ticketResponse{
		ID:          t.ID,
		Title:       t.Title,
		Description: t.Description,
		Status:      t.Status,
		CreatedAt:   t.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   t.UpdatedAt.Format(time.RFC3339),
	}
}

type createTicketRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

func (a *API) CreateTicket(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req createTicketRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	now := time.Now().UTC()
	ticket := store.Ticket{
		ID:          uuid.NewString(),
		UserID:      userID,
		Title:       req.Title,
		Description: req.Description,
		Status:      StatusOpen,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := a.Store.CreateTicket(ticket); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create ticket")
		return
	}

	writeJSON(w, http.StatusCreated, toTicketResponse(&ticket))
}

func (a *API) ListTickets(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	tickets, err := a.Store.ListTicketsByUser(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list tickets")
		return
	}

	resp := make([]ticketResponse, 0, len(tickets))
	for i := range tickets {
		resp = append(resp, toTicketResponse(&tickets[i]))
	}

	writeJSON(w, http.StatusOK, resp)
}

// getOwnedTicket loads a ticket by ID and ensures it belongs to the requesting user.
// Tickets that don't exist or belong to someone else are both reported as 404,
// so ticket existence cannot be probed by other users.
func (a *API) getOwnedTicket(w http.ResponseWriter, r *http.Request, id, userID string) (*store.Ticket, bool) {
	ticket, err := a.Store.GetTicketByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "ticket not found")
		return nil, false
	}
	if ticket.UserID != userID {
		writeError(w, http.StatusNotFound, "ticket not found")
		return nil, false
	}
	return ticket, true
}

func (a *API) GetTicket(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := r.PathValue("id")
	ticket, ok := a.getOwnedTicket(w, r, id, userID)
	if !ok {
		return
	}

	writeJSON(w, http.StatusOK, toTicketResponse(ticket))
}

type updateStatusRequest struct {
	Status string `json:"status"`
}

func (a *API) UpdateTicketStatus(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := r.PathValue("id")
	ticket, ok := a.getOwnedTicket(w, r, id, userID)
	if !ok {
		return
	}

	var req updateStatusRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	switch req.Status {
	case StatusOpen, StatusInProgress, StatusClosed:
	default:
		writeError(w, http.StatusBadRequest, "invalid status value")
		return
	}

	if allowedTransitions[ticket.Status] != req.Status {
		writeError(w, http.StatusBadRequest, "invalid status transition from "+ticket.Status+" to "+req.Status)
		return
	}

	now := time.Now().UTC()
	if err := a.Store.UpdateTicketStatus(ticket.ID, req.Status, now); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update ticket status")
		return
	}

	ticket.Status = req.Status
	ticket.UpdatedAt = now
	writeJSON(w, http.StatusOK, toTicketResponse(ticket))
}
