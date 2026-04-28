package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shuosc/scnet-server/internal/model"
)

var (
	ErrStudentIDTaken    = errors.New("student ID already taken")
	ErrInviteUnavailable = errors.New("invalid or expired invite code")
)

type UserStore interface {
	Create(ctx context.Context, user *model.User) error
	RegisterWithInvite(ctx context.Context, user *model.User, inviteCode string) error
	FindByStudentID(ctx context.Context, studentID string) (*model.User, error)
	FindByID(ctx context.Context, id uuid.UUID) (*model.User, error)
	UpdatePassword(ctx context.Context, userID uuid.UUID, passwordHash string) error
	Update(ctx context.Context, userID uuid.UUID, fields UserUpdateFields) error
	UpdateStatus(ctx context.Context, userID uuid.UUID, status string) error
	List(ctx context.Context, params UserListParams) (*UserListResult, error)
	Count(ctx context.Context) (int, error)
	CountByStatus(ctx context.Context) (map[string]int, error)
}

type OptionalStringUpdate struct {
	Set   bool
	Value *string
}

type UserUpdateFields struct {
	DisplayName OptionalStringUpdate
	Email       OptionalStringUpdate
	Phone       OptionalStringUpdate
	Wechat      OptionalStringUpdate
	Telegram    OptionalStringUpdate
	MaxPeers    *int
	Status      *string
}

type UserListParams struct {
	Page      int
	PageSize  int
	StudentID string
	Status    string
	Role      string
	SortBy    string
	SortOrder string
}

type UserListResult struct {
	Items []*model.User
	Total int
}

type userStore struct {
	pool *pgxpool.Pool
}

func NewUserStore(pool *pgxpool.Pool) UserStore {
	return &userStore{pool: pool}
}

func (s *userStore) Create(ctx context.Context, user *model.User) error {
	query := `
		INSERT INTO users (student_id, password, role, invite_id, display_name, email, phone, wechat, telegram, max_peers, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, created_at, updated_at`
	err := s.pool.QueryRow(ctx, query,
		user.StudentID, user.Password, user.Role, user.InviteID,
		user.DisplayName, user.Email, user.Phone, user.Wechat,
		user.Telegram, user.MaxPeers, user.Status,
	).Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

func (s *userStore) RegisterWithInvite(ctx context.Context, user *model.User, inviteCode string) (err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin registration transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	var inviteID uuid.UUID
	var maxUses int
	var useCount int
	var expiresAt *time.Time
	query := `
		SELECT id, max_uses, use_count, expires_at
		FROM invite_codes
		WHERE code = $1
		FOR UPDATE`
	if scanErr := tx.QueryRow(ctx, query, inviteCode).Scan(&inviteID, &maxUses, &useCount, &expiresAt); scanErr != nil {
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return ErrInviteUnavailable
		}
		return fmt.Errorf("find invite by code: %w", scanErr)
	}
	if useCount >= maxUses || (expiresAt != nil && !expiresAt.After(time.Now())) {
		return ErrInviteUnavailable
	}

	user.InviteID = inviteID
	insertQuery := `
		INSERT INTO users (student_id, password, role, invite_id, display_name, email, phone, wechat, telegram, max_peers, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, created_at, updated_at`
	if scanErr := tx.QueryRow(ctx, insertQuery,
		user.StudentID, user.Password, user.Role, user.InviteID,
		user.DisplayName, user.Email, user.Phone, user.Wechat,
		user.Telegram, user.MaxPeers, user.Status,
	).Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt); scanErr != nil {
		if isUniqueViolation(scanErr) {
			return ErrStudentIDTaken
		}
		return fmt.Errorf("create user: %w", scanErr)
	}

	tag, execErr := tx.Exec(ctx, `UPDATE invite_codes SET use_count = use_count + 1, used_by = $1 WHERE id = $2`, user.ID, inviteID)
	if execErr != nil {
		return fmt.Errorf("mark invite used: %w", execErr)
	}
	if tag.RowsAffected() != 1 {
		return ErrInviteUnavailable
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit registration transaction: %w", err)
	}
	return nil
}

func (s *userStore) FindByStudentID(ctx context.Context, studentID string) (*model.User, error) {
	query := `
		SELECT id, student_id, password, role, invite_id, display_name, email, phone, wechat, telegram, max_peers, status, created_at, updated_at
		FROM users WHERE student_id = $1`
	var user model.User
	err := s.pool.QueryRow(ctx, query, studentID).Scan(
		&user.ID, &user.StudentID, &user.Password, &user.Role, &user.InviteID,
		&user.DisplayName, &user.Email, &user.Phone, &user.Wechat,
		&user.Telegram, &user.MaxPeers, &user.Status, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("find user by student_id: %w", err)
	}
	return &user, nil
}

func (s *userStore) FindByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	query := `
		SELECT id, student_id, password, role, invite_id, display_name, email, phone, wechat, telegram, max_peers, status, created_at, updated_at
		FROM users WHERE id = $1`
	var user model.User
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&user.ID, &user.StudentID, &user.Password, &user.Role, &user.InviteID,
		&user.DisplayName, &user.Email, &user.Phone, &user.Wechat,
		&user.Telegram, &user.MaxPeers, &user.Status, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("find user by id: %w", err)
	}
	return &user, nil
}

func (s *userStore) UpdatePassword(ctx context.Context, userID uuid.UUID, passwordHash string) error {
	query := `UPDATE users SET password = $1, updated_at = NOW() WHERE id = $2`
	_, err := s.pool.Exec(ctx, query, passwordHash, userID)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	return nil
}

func (s *userStore) Update(ctx context.Context, userID uuid.UUID, fields UserUpdateFields) error {
	var setClauses []string
	var args []interface{}
	argIdx := 1

	setClauses, args, argIdx = appendOptionalStringUpdate(setClauses, args, argIdx, "display_name", fields.DisplayName)
	setClauses, args, argIdx = appendOptionalStringUpdate(setClauses, args, argIdx, "email", fields.Email)
	setClauses, args, argIdx = appendOptionalStringUpdate(setClauses, args, argIdx, "phone", fields.Phone)
	setClauses, args, argIdx = appendOptionalStringUpdate(setClauses, args, argIdx, "wechat", fields.Wechat)
	setClauses, args, argIdx = appendOptionalStringUpdate(setClauses, args, argIdx, "telegram", fields.Telegram)
	if fields.MaxPeers != nil {
		setClauses = append(setClauses, fmt.Sprintf("max_peers = $%d", argIdx))
		args = append(args, *fields.MaxPeers)
		argIdx++
	}
	if fields.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, *fields.Status)
		argIdx++
	}

	if len(setClauses) == 0 {
		return nil
	}

	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, userID)

	query := fmt.Sprintf("UPDATE users SET %s WHERE id = $%d", strings.Join(setClauses, ", "), argIdx)
	_, err := s.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	return nil
}

func (s *userStore) UpdateStatus(ctx context.Context, userID uuid.UUID, status string) error {
	query := `UPDATE users SET status = $1, updated_at = NOW() WHERE id = $2`
	_, err := s.pool.Exec(ctx, query, status, userID)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	return nil
}

func appendOptionalStringUpdate(setClauses []string, args []interface{}, argIdx int, column string, field OptionalStringUpdate) ([]string, []interface{}, int) {
	if !field.Set {
		return setClauses, args, argIdx
	}
	if field.Value == nil {
		setClauses = append(setClauses, fmt.Sprintf("%s = NULL", column))
		return setClauses, args, argIdx
	}

	setClauses = append(setClauses, fmt.Sprintf("%s = $%d", column, argIdx))
	args = append(args, *field.Value)
	return setClauses, args, argIdx + 1
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (s *userStore) List(ctx context.Context, params UserListParams) (*UserListResult, error) {
	var conditions []string
	var args []interface{}
	argIdx := 1

	if params.StudentID != "" {
		conditions = append(conditions, fmt.Sprintf("student_id LIKE $%d", argIdx))
		args = append(args, "%"+params.StudentID+"%")
		argIdx++
	}
	if params.Status != "" {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, params.Status)
		argIdx++
	}
	if params.Role != "" {
		conditions = append(conditions, fmt.Sprintf("role = $%d", argIdx))
		args = append(args, params.Role)
		argIdx++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	sortBy := "created_at"
	if params.SortBy == "student_id" || params.SortBy == "status" || params.SortBy == "role" || params.SortBy == "display_name" {
		sortBy = params.SortBy
	}
	sortOrder := "DESC"
	if params.SortOrder == "ASC" {
		sortOrder = "ASC"
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM users %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count users: %w", err)
	}

	offset := (params.Page - 1) * params.PageSize
	listQuery := fmt.Sprintf(
		`SELECT id, student_id, password, role, invite_id, display_name, email, phone, wechat, telegram, max_peers, status, created_at, updated_at
		FROM users %s ORDER BY %s %s LIMIT $%d OFFSET $%d`,
		whereClause, sortBy, sortOrder, argIdx, argIdx+1,
	)
	args = append(args, params.PageSize, offset)

	rows, err := s.pool.Query(ctx, listQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var items []*model.User
	for rows.Next() {
		var user model.User
		if err := rows.Scan(
			&user.ID, &user.StudentID, &user.Password, &user.Role, &user.InviteID,
			&user.DisplayName, &user.Email, &user.Phone, &user.Wechat,
			&user.Telegram, &user.MaxPeers, &user.Status, &user.CreatedAt, &user.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		items = append(items, &user)
	}

	return &UserListResult{Items: items, Total: total}, nil
}

func (s *userStore) Count(ctx context.Context) (int, error) {
	var count int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return count, nil
}

func (s *userStore) CountByStatus(ctx context.Context) (map[string]int, error) {
	query := `SELECT status, COUNT(*) FROM users GROUP BY status`
	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("count by status: %w", err)
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("scan status count: %w", err)
		}
		result[status] = count
	}

	for _, key := range []string{"active", "suspended", "deleted"} {
		if _, ok := result[key]; !ok {
			result[key] = 0
		}
	}

	return result, nil
}
