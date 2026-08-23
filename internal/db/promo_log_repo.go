package db

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/GenshIv/makoshop/internal/model"
)

type PromoLogRepo struct {
	store *Store
}

func NewPromoLogRepo(store *Store) *PromoLogRepo {
	return &PromoLogRepo{store: store}
}

func (r *PromoLogRepo) Create(l *model.PromoLog) error {
	id, err := r.store.NextID("promo_log")
	if err != nil {
		return fmt.Errorf("next_id promo_log: %w", err)
	}
	l.ID = id
	l.CreatedAt = time.Now().Unix()

	data := MarshalPromoLog(*l)
	if err := r.store.DocPut(KeyPromoLog(l.ID), data); err != nil {
		return fmt.Errorf("save promo_log: %w", err)
	}

	// Index by campaign (turbo)
	campaignKey := fmt.Sprintf("promo_log_campaign:%d", l.CampaignID)
	if _, err := r.store.db.TurboPutIndexString(campaignKey, strconv.Itoa(int(l.ID))); err != nil {
		_ = r.store.DocDelete(KeyPromoLog(l.ID))
		return fmt.Errorf("turbo index log by campaign: %w", err)
	}

	return nil
}

func (r *PromoLogRepo) Get(id int64) (*model.PromoLog, error) {
	data, err := r.store.DocGet(KeyPromoLog(id))
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("promo_log %d not found", id)
		}
		return nil, fmt.Errorf("get promo_log %d: %w", id, err)
	}
	return UnmarshalPromoLog(data)
}

func (r *PromoLogRepo) ListByCampaign(campaignID int64) ([]model.PromoLog, error) {
	key := fmt.Sprintf("promo_log_campaign:%d", campaignID)
	tokens, err := r.store.db.TurboGetIndexTokens(key)
	if err != nil || len(tokens) == 0 {
		return nil, nil
	}

	docs, err := r.store.db.MultiGetByDocIDsWithPrefix(tokens, "promo_log:")
	if err != nil {
		return nil, fmt.Errorf("multi get promo logs: %w", err)
	}

	var result []model.PromoLog
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		l, err := UnmarshalPromoLog(doc)
		if err != nil {
			continue
		}
		result = append(result, *l)
	}

	return result, nil
}

// GetAllLogs returns all logs. Currently not implemented without ForEach.
// Use ListByCampaign instead.
func (r *PromoLogRepo) GetAllLogs() ([]model.PromoLog, error) {
	return nil, nil
}
