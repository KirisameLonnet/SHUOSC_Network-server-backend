package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shuosc/scnet-server/internal/model"
)

type InviteStore interface {
	Create(ctx context.Context, invite *model.InviteCode) error
	FindByCode(ctx context.Context, code string) (*model.InviteCode, error)
	FindByID(ctx context.Context, id uuid.UUID) (*model.InviteCode, error)
	MarkUsed(ctx context.Context, inviteID uuid.UUID, usedBy uuid.UUID) error
	Update(ctx context.Context, inviteID uuid.UUID, fields InviteUpdateFields) error
	Delete(ctx context.Context, inviteID uuid.UUID) error
	List(ctx context.Context, params InviteListParams) (*InviteListResult, error)
	CountByState(ctx context.Context) (map[string]int, error)
}

type InviteUpdateFields struct {
	MaxUses        *int
	ExpiresAt      *time.Time
	ClearExpiresAt bool
}

type InviteListParams struct {
	Page     int
	PageSize int
	Code     string
	State    string
}

type InviteListResult struct {
	Items []*model.InviteCode
	Total int
}

type inviteStore struct {
	pool *pgxpool.Pool
}

func NewInviteStore(pool *pgxpool.Pool) InviteStore {
	return &inviteStore{pool: pool}
}

func (s *inviteStore) Create(ctx context.Context, invite *model.InviteCode) error {
	query := `
		INSERT INTO invite_codes (code, created_by, max_uses, expires_at)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at`
	err := s.pool.QueryRow(ctx, query,
		invite.Code, invite.CreatedBy, invite.MaxUses, invite.ExpiresAt,
	).Scan(&invite.ID, &invite.CreatedAt)
	if err != nil {
		return fmt.Errorf("create invite: %w", err)
	}
	return nil
}

func (s *inviteStore) FindByCode(ctx context.Context, code string) (*model.InviteCode, error) {
	query := `
		SELECT id, code, created_by, used_by, max_uses, use_count, expires_at, created_at
		FROM invite_codes WHERE code = $1`
	var invite model.InviteCode
	err := s.pool.QueryRow(ctx, query, code).Scan(
		&invite.ID, &invite.Code, &invite.CreatedBy, &invite.UsedBy,
		&invite.MaxUses, &invite.UseCount, &invite.ExpiresAt, &invite.CreatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("find invite by code: %w", err)
	}
	return &invite, nil
}

func (s *inviteStore) FindByID(ctx context.Context, id uuid.UUID) (*model.InviteCode, error) {
	query := `
		SELECT id, code, created_by, used_by, max_uses, use_count, expires_at, created_at
		FROM invite_codes WHERE id = $1`
	var invite model.InviteCode
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&invite.ID, &invite.Code, &invite.CreatedBy, &invite.UsedBy,
		&invite.MaxUses, &invite.UseCount, &invite.ExpiresAt, &invite.CreatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("find invite by id: %w", err)
	}
	return &invite, nil
}

func (s *inviteStore) MarkUsed(ctx context.Context, inviteID uuid.UUID, usedBy uuid.UUID) error {
	query := `UPDATE invite_codes SET use_count = use_count + 1, used_by = $1 WHERE id = $2`
	_, err := s.pool.Exec(ctx, query, usedBy, inviteID)
	if err != nil {
		return fmt.Errorf("mark invite used: %w", err)
	}
	return nil
}

func (s *inviteStore) Update(ctx context.Context, inviteID uuid.UUID, fields InviteUpdateFields) error {
	var sets []string
	var args []interface{}
	argIdx := 1

	if fields.MaxUses != nil {
		sets = append(sets, fmt.Sprintf("max_uses = $%d", argIdx))
		args = append(args, *fields.MaxUses)
		argIdx++
	}
	if fields.ClearExpiresAt {
		sets = append(sets, "expires_at = NULL")
	} else if fields.ExpiresAt != nil {
		sets = append(sets, fmt.Sprintf("expires_at = $%d", argIdx))
		args = append(args, *fields.ExpiresAt)
		argIdx++
	}
	if len(sets) == 0 {
		return nil
	}

	query := fmt.Sprintf("UPDATE invite_codes SET %s WHERE id = $%d", strings.Join(sets, ", "), argIdx)
	args = append(args, inviteID)
	if _, err := s.pool.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("update invite: %w", err)
	}
	return nil
}

func (s *inviteStore) Delete(ctx context.Context, inviteID uuid.UUID) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM invite_codes WHERE id = $1`, inviteID); err != nil {
		return fmt.Errorf("delete invite: %w", err)
	}
	return nil
}

func (s *inviteStore) List(ctx context.Context, params InviteListParams) (*InviteListResult, error) {
	var conditions []string
	var args []interface{}
	argIdx := 1

	if params.Code != "" {
		conditions = append(conditions, fmt.Sprintf("code LIKE $%d", argIdx))
		args = append(args, "%"+params.Code+"%")
		argIdx++
	}

	if params.State != "" {
		switch params.State {
		case "available":
			conditions = append(conditions, fmt.Sprintf("max_uses > use_count AND (expires_at IS NULL OR expires_at > NOW())"))
		case "used_up":
			conditions = append(conditions, fmt.Sprintf("max_uses <= use_count"))
		case "expired":
			conditions = append(conditions, fmt.Sprintf("expires_at IS NOT NULL AND expires_at <= NOW() AND max_uses > use_count"))
		}
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM invite_codes %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count invites: %w", err)
	}

	offset := (params.Page - 1) * params.PageSize
	listQuery := fmt.Sprintf(
		`SELECT id, code, created_by, used_by, max_uses, use_count, expires_at, created_at
		FROM invite_codes %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`,
		whereClause, argIdx, argIdx+1,
	)
	args = append(args, params.PageSize, offset)

	rows, err := s.pool.Query(ctx, listQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	defer rows.Close()

	var items []*model.InviteCode
	for rows.Next() {
		var invite model.InviteCode
		if err := rows.Scan(
			&invite.ID, &invite.Code, &invite.CreatedBy, &invite.UsedBy,
			&invite.MaxUses, &invite.UseCount, &invite.ExpiresAt, &invite.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan invite: %w", err)
		}
		items = append(items, &invite)
	}

	return &InviteListResult{Items: items, Total: total}, nil
}

func (s *inviteStore) CountByState(ctx context.Context) (map[string]int, error) {
	query := `
		SELECT
			CASE
				WHEN max_uses <= use_count THEN 'used_up'
				WHEN expires_at IS NOT NULL AND expires_at <= NOW() AND max_uses > use_count THEN 'expired'
				ELSE 'available'
			END AS state,
			COUNT(*) AS count
		FROM invite_codes
		GROUP BY state`
	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("count by state: %w", err)
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var state string
		var count int
		if err := rows.Scan(&state, &count); err != nil {
			return nil, fmt.Errorf("scan state count: %w", err)
		}
		result[state] = count
	}

	for _, key := range []string{"available", "used_up", "expired"} {
		if _, ok := result[key]; !ok {
			result[key] = 0
		}
	}

	return result, nil
}
