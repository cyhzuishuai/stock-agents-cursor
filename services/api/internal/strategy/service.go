package strategy

import (
	"context"
	"errors"
	"fmt"

	"github.com/cyh/stock-agents/services/api/internal/models"
	"gorm.io/gorm"
)

var (
	ErrNotFound   = errors.New("strategy not found")
	ErrForbidden  = errors.New("strategy operation forbidden")
	ErrValidation = errors.New("strategy validation failed")
)

type Service struct{ DB *gorm.DB }

type CreateInput struct {
	Name                 string
	Description          string
	PreOpenMinutes       int
	IntradayEveryMinutes int
	IntradayStartET      string
	IntradayEndET        string
	ExecutionMode        string
}

type UpdateInput struct {
	Name                 string
	Description          string
	PreOpenMinutes       int
	IntradayEveryMinutes int
	IntradayStartET      string
	IntradayEndET        string
	ExecutionMode        string
}

func (s *Service) List(ctx context.Context) ([]models.Strategy, error) {
	var strategies []models.Strategy
	err := s.DB.WithContext(ctx).Order("id ASC").Find(&strategies).Error
	return strategies, err
}

func (s *Service) Get(ctx context.Context, id uint) (models.Strategy, error) {
	var st models.Strategy
	err := s.DB.WithContext(ctx).First(&st, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Strategy{}, ErrNotFound
	}
	return st, err
}

func (s *Service) Create(ctx context.Context, in CreateInput) (models.Strategy, error) {
	st := models.Strategy{
		Name:                 in.Name,
		Description:          in.Description,
		PreOpenMinutes:       in.PreOpenMinutes,
		IntradayEveryMinutes: in.IntradayEveryMinutes,
		IntradayStartET:      in.IntradayStartET,
		IntradayEndET:        in.IntradayEndET,
		ExecutionMode:        in.ExecutionMode,
		IsActive:             false,
		IsSystemDefault:      false,
	}
	if err := validateStrategy(st); err != nil {
		return models.Strategy{}, err
	}
	if err := s.DB.WithContext(ctx).Create(&st).Error; err != nil {
		return models.Strategy{}, err
	}
	return st, nil
}

func (s *Service) Update(ctx context.Context, id uint, in UpdateInput) (models.Strategy, error) {
	var st models.Strategy
	if err := s.DB.WithContext(ctx).First(&st, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.Strategy{}, ErrNotFound
		}
		return models.Strategy{}, err
	}
	st.Name = in.Name
	st.Description = in.Description
	st.PreOpenMinutes = in.PreOpenMinutes
	st.IntradayEveryMinutes = in.IntradayEveryMinutes
	st.IntradayStartET = in.IntradayStartET
	st.IntradayEndET = in.IntradayEndET
	st.ExecutionMode = in.ExecutionMode
	if err := validateStrategy(st); err != nil {
		return models.Strategy{}, err
	}
	if err := s.DB.WithContext(ctx).Save(&st).Error; err != nil {
		return models.Strategy{}, err
	}
	return st, nil
}

func (s *Service) Activate(ctx context.Context, id uint) (models.Strategy, error) {
	var st models.Strategy
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Strategy{}).Where("is_active = ?", true).Update("is_active", false).Error; err != nil {
			return err
		}
		res := tx.Model(&models.Strategy{}).Where("id = ?", id).Update("is_active", true)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return tx.First(&st, id).Error
	})
	if err != nil {
		return models.Strategy{}, err
	}
	return st, nil
}

func (s *Service) Delete(ctx context.Context, id uint) error {
	var st models.Strategy
	if err := s.DB.WithContext(ctx).First(&st, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	if st.IsSystemDefault || st.IsActive {
		return ErrForbidden
	}
	return s.DB.WithContext(ctx).Delete(&st).Error
}

func (s *Service) Active(ctx context.Context) (*models.Strategy, error) {
	var st models.Strategy
	err := s.DB.WithContext(ctx).Where("is_active = ?", true).First(&st).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &st, nil
}

func validateStrategy(st models.Strategy) error {
	if err := ValidateStrategyFields(st); err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return nil
}
