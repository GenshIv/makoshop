package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/GenshIv/makoshop/internal/auth"
	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/makoshop/internal/model"
)

// HandleCommentCreate creates a new comment.
// POST /comments
func (h *Handlers) HandleCommentCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser {
		httpres.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	var req struct {
		TargetType string `json:"target_type"`
		TargetID   int64  `json:"target_id"`
		ParentID   int64  `json:"parent_id,omitempty"`
		Content    string `json:"content"`
	}
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	if req.TargetType == "" || req.TargetID <= 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "target_type and target_id are required")
		return
	}
	if req.Content == "" || len(req.Content) > 5000 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "content is required (max 5000 chars)")
		return
	}

	comment := &model.Comment{
		TargetType: model.CommentTargetType(req.TargetType),
		TargetID:   req.TargetID,
		UserID:     ctxUser.ID,
		ParentID:   req.ParentID,
		Content:    req.Content,
		Status:     model.CommentStatusApproved,
	}

	if err := h.commentRepo.Create(comment); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusCreated, comment)
}

// HandleCommentsList returns comments for a target.
// GET /comments?target_type=product&target_id=123&page=1&limit=50
func (h *Handlers) HandleCommentsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	targetType := r.URL.Query().Get("target_type")
	targetIDStr := r.URL.Query().Get("target_id")

	if targetType == "" || targetIDStr == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "target_type and target_id are required")
		return
	}

	targetID, err := strconv.ParseInt(targetIDStr, 10, 64)
	if err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid target_id")
		return
	}

	page, _ := parseQueryInt(r.URL.Query().Get("page"), 1)
	limit, _ := parseQueryInt(r.URL.Query().Get("limit"), 50)
	statusFilter := r.URL.Query().Get("status")

	comments, total, err := h.commentRepo.ListByTarget(targetType, targetID, page, limit, statusFilter)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if comments == nil {
		comments = []model.Comment{}
	}

	// Enrich with user names
	type CommentWithUser struct {
		model.Comment
		UserName string `json:"user_name,omitempty"`
	}

	var enriched []CommentWithUser
	for _, c := range comments {
		user, _ := h.userRepo.GetByID(c.UserID)
		cw := CommentWithUser{Comment: c}
		if user != nil {
			cw.UserName = user.Profile.Name
			if cw.UserName == "" {
				cw.UserName = user.Email
			}
		}
		enriched = append(enriched, cw)
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"items": enriched,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// HandleVoteCreate votes on a comment or review.
// POST /votes
func (h *Handlers) HandleVoteCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser {
		httpres.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	var req struct {
		TargetType string         `json:"target_type"` // "comment" or "review"
		TargetID   int64          `json:"target_id"`
		VoteType   model.VoteType `json:"vote_type"` // "like" or "dislike"
	}
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	if req.TargetType == "" || req.TargetID <= 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "target_type and target_id are required")
		return
	}
	if req.VoteType != model.VoteLike && req.VoteType != model.VoteDislike {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "vote_type must be like or dislike")
		return
	}

	vote := &model.Vote{
		TargetType: req.TargetType,
		TargetID:   req.TargetID,
		UserID:     ctxUser.ID,
		VoteType:   req.VoteType,
	}

	if err := h.voteRepo.Create(vote); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Update like/dislike counts if voting on a comment or eanpage
	if req.TargetType == "comment" {
		h.recalculateCommentVoteCounts(req.TargetID)
	} else if req.TargetType == "eanpage" {
		h.recalculateEanPageVoteCounts(req.TargetID)
	}

	// Get updated vote state
	userVote, _ := h.voteRepo.GetVoteForTarget(req.TargetType, req.TargetID, ctxUser.ID)

	httpres.WriteJSON(w, http.StatusOK, userVote)
}

// HandleVoteCheck checks if a user has voted on a target.
// GET /votes/check?target_type=comment&target_id=123
func (h *Handlers) HandleVoteCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser {
		httpres.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	targetType := r.URL.Query().Get("target_type")
	targetIDStr := r.URL.Query().Get("target_id")

	if targetType == "" || targetIDStr == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "target_type and target_id are required")
		return
	}

	targetID, err := strconv.ParseInt(targetIDStr, 10, 64)
	if err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid target_id")
		return
	}

	userVote, err := h.voteRepo.GetVoteForTarget(targetType, targetID, ctxUser.ID)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusOK, userVote)
}

// HandleAdminCommentsList returns paginated comments for admin.
// GET /admin/comments?page=1&limit=50&status=approved&target_type=product
func (h *Handlers) HandleAdminCommentsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	page, _ := parseQueryInt(r.URL.Query().Get("page"), 1)
	limit, _ := parseQueryInt(r.URL.Query().Get("limit"), 50)
	statusFilter := r.URL.Query().Get("status")
	targetTypeFilter := r.URL.Query().Get("target_type")

	comments, total, err := h.commentRepo.ListAll(page, limit, statusFilter, targetTypeFilter)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if comments == nil {
		comments = []model.Comment{}
	}

	// Enrich with user names
	type CommentWithUser struct {
		model.Comment
		UserName string `json:"user_name,omitempty"`
	}

	var enriched []CommentWithUser
	for _, c := range comments {
		user, _ := h.userRepo.GetByID(c.UserID)
		cw := CommentWithUser{Comment: c}
		if user != nil {
			cw.UserName = user.Profile.Name
			if cw.UserName == "" {
				cw.UserName = user.Email
			}
		}
		enriched = append(enriched, cw)
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"items": enriched,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// HandleAdminCommentUpdate updates a comment (moderation).
// PATCH /admin/comments/{id}
func (h *Handlers) HandleAdminCommentUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[3] == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "id is required")
		return
	}

	id, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid id")
		return
	}

	var updates map[string]interface{}
	if !httpres.ReadJSON(w, r, &updates) {
		return
	}

	if _, ok := updates["status"].(string); ok {
		newStatus := model.CommentStatus(updates["status"].(string))
		if err := h.commentRepo.UpdateStatus(id, newStatus); err != nil {
			httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
	}

	if _, ok := updates["is_featured"].(bool); ok {
		if err := h.commentRepo.UpdateFeatured(id, updates["is_featured"].(bool)); err != nil {
			httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
	}

	updated, _ := h.commentRepo.Get(id)
	httpres.WriteJSON(w, http.StatusOK, updated)
}

// HandleAdminCommentDelete deletes a comment.
// DELETE /admin/comments/{id}
func (h *Handlers) HandleAdminCommentDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[3] == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "id is required")
		return
	}

	id, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid id")
		return
	}

	if err := h.commentRepo.Delete(id); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// HandleAdminCommentsBulkActions performs bulk actions on comments.
// POST /admin/comments/bulk-actions
func (h *Handlers) HandleAdminCommentsBulkActions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req struct {
		Action string  `json:"action"` // "approve", "reject", "hide", "delete"
		IDs    []int64 `json:"ids"`
	}

	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	if len(req.IDs) == 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "ids are required")
		return
	}

	processed := 0
	var errors []string

	for _, id := range req.IDs {
		switch req.Action {
		case "approve":
			if err := h.commentRepo.UpdateStatus(id, model.CommentStatusApproved); err != nil {
				errors = append(errors, err.Error())
			} else {
				processed++
			}
		case "reject":
			if err := h.commentRepo.UpdateStatus(id, model.CommentStatusRejected); err != nil {
				errors = append(errors, err.Error())
			} else {
				processed++
			}
		case "hide":
			if err := h.commentRepo.UpdateStatus(id, model.CommentStatusHidden); err != nil {
				errors = append(errors, err.Error())
			} else {
				processed++
			}
		case "delete":
			if err := h.commentRepo.Delete(id); err != nil {
				errors = append(errors, err.Error())
			} else {
				processed++
			}
		default:
			errors = append(errors, "unknown action: "+req.Action)
		}
	}

	resp := map[string]interface{}{
		"processed": processed,
		"total":     len(req.IDs),
	}
	if len(errors) > 0 {
		resp["errors"] = errors
	}

	httpres.WriteJSON(w, http.StatusOK, resp)
}

// HandleAdminCommentStats returns comment statistics.
// GET /admin/comments/stats
func (h *Handlers) HandleAdminCommentStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	stats, err := h.commentRepo.GetStats()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusOK, stats)
}

// HandleAdminVoteStats returns vote statistics.
// GET /admin/votes/stats
func (h *Handlers) HandleAdminVoteStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	stats, err := h.voteRepo.GetStats()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusOK, stats)
}

// recalculateCommentVoteCounts recalculates like_count and dislike_count for a comment.
func (h *Handlers) recalculateCommentVoteCounts(commentID int64) {
	// Get all votes for this comment
	// We need to iterate through all votes and count
	// For now, we'll use a simplified approach
	// In production, you'd want a more efficient query

	// Get votes from turbo index
	voteKey := "vote_target:comment:" + strconv.FormatInt(commentID, 10)
	tokens, err := h.store.DB().TurboGetIndexTokens(voteKey)
	if err != nil || len(tokens) == 0 {
		// No votes - reset counts
		h.commentRepo.UpdateLikeCount(commentID, 0, 0)
		return
	}

	docs, err := h.store.DB().MultiGetByDocIDs(tokens)
	if err != nil {
		return
	}

	var likes, dislikes int
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		vote, err := db.UnmarshalVote(doc)
		if err != nil {
			continue
		}
		if vote.VoteType == model.VoteLike {
			likes++
		} else {
			dislikes++
		}
	}

	h.commentRepo.UpdateLikeCount(commentID, likes, dislikes)
}

// recalculateEanPageVoteCounts recalculates like_count and dislike_count for an eanpage.
func (h *Handlers) recalculateEanPageVoteCounts(eanPageID int64) {
	// Get votes from turbo index
	voteKey := "vote_target:eanpage:" + strconv.FormatInt(eanPageID, 10)
	tokens, err := h.store.DB().TurboGetIndexTokens(voteKey)
	if err != nil || len(tokens) == 0 {
		// No votes - reset counts
		h.eanPageRepo.UpdateLikeDislikeCount(eanPageID, 0, 0)
		return
	}

	docs, err := h.store.DB().MultiGetByDocIDs(tokens)
	if err != nil {
		return
	}

	var likes, dislikes int
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		vote, err := db.UnmarshalVote(doc)
		if err != nil {
			continue
		}
		if vote.VoteType == model.VoteLike {
			likes++
		} else {
			dislikes++
		}
	}

	h.eanPageRepo.UpdateLikeDislikeCount(eanPageID, likes, dislikes)
}
