package data

import (
	"encoding/json"
	"errors"
	"github.com/GMWalletApp/epusdt/config"
	"github.com/GMWalletApp/epusdt/model/dao"
	"github.com/GMWalletApp/epusdt/model/mdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func LoadVerifiedRateState(base string) (config.VerifiedRateState, bool, error) {
	var row mdb.VerifiedRateCache
	err := dao.Mdb.Where("base = ?", base).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return config.VerifiedRateState{}, false, nil
	}
	if err != nil {
		return config.VerifiedRateState{}, false, err
	}
	var state config.VerifiedRateState
	if err = json.Unmarshal([]byte(row.State), &state); err != nil {
		return state, false, err
	}
	return state, true, nil
}
func SaveVerifiedRateState(state config.VerifiedRateState) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return dao.Mdb.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "base"}}, DoUpdates: clause.AssignmentColumns([]string{"state", "updated_at"})}).Create(&mdb.VerifiedRateCache{Base: state.Base, State: string(raw)}).Error
}
