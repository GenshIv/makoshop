package db

import (
	"errors"
	"fmt"
	"time"

	"github.com/GenshIv/makoshop/internal/model"
)

type VoteRepo struct {
	store *Store
}

func NewVoteRepo(store *Store) *VoteRepo {
	return &VoteRepo{store: store}
}

// Create creates a new vote (like/dislike).
func (r *VoteRepo) Create(vote *model.Vote) error {
	// Check if user already voted on this target
	existing, err := r.GetVoteByTargetAndUser(vote.TargetType, vote.TargetID, vote.UserID)
	if err == nil && existing != nil {
		// User already voted - update the vote
		return r.UpdateVote(existing, vote.VoteType)
	}

	id, err := r.store.NextID("vote")
	if err != nil {
		return fmt.Errorf("next_id vote: %w", err)
	}
	vote.ID = id
	vote.CreatedAt = time.Now().Unix()

	data := MarshalVote(*vote)
	if err := r.store.DocPut(KeyVote(vote.ID), data); err != nil {
		return fmt.Errorf("save vote: %w", err)
	}

	// Index: vote by target
	targetKey := fmt.Sprintf("vote_target:%s:%d", vote.TargetType, vote.TargetID)
	if _, err := r.store.db.TurboPutIndexString(targetKey, KeyVote(vote.ID)); err != nil {
		_ = r.store.DocDelete(KeyVote(vote.ID))
		return fmt.Errorf("turbo index vote by target: %w", err)
	}

	// Index: vote by user
	userKey := fmt.Sprintf("vote_user:%d", vote.UserID)
	if _, err := r.store.db.TurboPutIndexString(userKey, KeyVote(vote.ID)); err != nil {
		_, _ = r.store.db.TurboDeleteIndexString(targetKey, KeyVote(vote.ID))
		_ = r.store.DocDelete(KeyVote(vote.ID))
		return fmt.Errorf("turbo index vote by user: %w", err)
	}

	return nil
}

// Get returns a vote by ID.
func (r *VoteRepo) Get(id int64) (*model.Vote, error) {
	data, err := r.store.DocGet(KeyVote(id))
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("vote %d not found", id)
		}
		return nil, fmt.Errorf("get vote %d: %w", id, err)
	}
	return UnmarshalVote(data)
}

// GetVoteByTargetAndUser returns the vote for a target and user, or nil if not found.
func (r *VoteRepo) GetVoteByTargetAndUser(targetType string, targetID, userID int64) (*model.Vote, error) {
	targetKey := fmt.Sprintf("vote_target:%s:%d", targetType, targetID)
	tokens, err := r.store.db.TurboGetIndexTokens(targetKey)
	if err != nil || len(tokens) == 0 {
		return nil, nil
	}

	docs, err := r.store.db.MultiGetByDocIDs(tokens)
	if err != nil {
		return nil, err
	}

	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		vote, err := UnmarshalVote(doc)
		if err != nil {
			continue
		}
		if vote.UserID == userID {
			return vote, nil
		}
	}

	return nil, nil
}

// UpdateVote updates an existing vote's type.
func (r *VoteRepo) UpdateVote(vote *model.Vote, newVoteType model.VoteType) error {
	vote.VoteType = newVoteType
	vote.UpdatedAt = time.Now().Unix()

	data := MarshalVote(*vote)
	if err := r.store.DocPut(KeyVote(vote.ID), data); err != nil {
		return fmt.Errorf("update vote: %w", err)
	}

	return nil
}

// Delete removes a vote.
func (r *VoteRepo) Delete(id int64) error {
	vote, err := r.Get(id)
	if err != nil {
		return err
	}

	targetKey := fmt.Sprintf("vote_target:%s:%d", vote.TargetType, vote.TargetID)
	userKey := fmt.Sprintf("vote_user:%d", vote.UserID)
	_, _ = r.store.db.TurboDeleteIndexString(targetKey, KeyVote(id))
	_, _ = r.store.db.TurboDeleteIndexString(userKey, KeyVote(id))

	if err := r.store.DocDelete(KeyVote(id)); err != nil {
		return fmt.Errorf("delete vote: %w", err)
	}
	return nil
}

// GetVoteForTarget returns the user's vote on a specific target.
func (r *VoteRepo) GetVoteForTarget(targetType string, targetID, userID int64) (*model.UserVote, error) {
	vote, err := r.GetVoteByTargetAndUser(targetType, targetID, userID)
	if err != nil {
		return nil, err
	}
	if vote == nil {
		return &model.UserVote{
			TargetType: targetType,
			TargetID:   targetID,
		}, nil
	}
	return &model.UserVote{
		TargetType: targetType,
		TargetID:   targetID,
		VoteType:   vote.VoteType,
	}, nil
}

// GetStats returns vote statistics.
type VoteStats struct {
	TotalVotes    int `json:"total_votes"`
	TotalLikes    int `json:"total_likes"`
	TotalDislikes int `json:"total_dislikes"`
}

func (r *VoteRepo) GetStats() (*VoteStats, error) {
	stats := &VoteStats{}

	// Get all votes from all target indexes
	allTargetKeys := []string{
		"vote_target:comment:",
		"vote_target:review:",
	}

	for _, prefix := range allTargetKeys {
		tokens, err := r.store.db.TurboGetIndexTokens(prefix)
		if err != nil || len(tokens) == 0 {
			continue
		}

		docs, err := r.store.db.MultiGetByDocIDs(tokens)
		if err != nil {
			continue
		}

		for _, doc := range docs {
			if len(doc) == 0 {
				continue
			}
			vote, err := UnmarshalVote(doc)
			if err != nil {
				continue
			}
			stats.TotalVotes++
			if vote.VoteType == model.VoteLike {
				stats.TotalLikes++
			} else {
				stats.TotalDislikes++
			}
		}
	}

	return stats, nil
}
