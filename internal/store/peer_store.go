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

type PeerStore interface {
	Create(ctx context.Context, peer *model.Peer) error
	FindActiveByUserID(ctx context.Context, userID uuid.UUID) ([]*model.Peer, error)
	FindByPublicKey(ctx context.Context, publicKey string) (*model.Peer, error)
	FindByID(ctx context.Context, id uuid.UUID) (*model.Peer, error)
	ListActive(ctx context.Context) ([]*model.Peer, error)
	CountActiveByUserID(ctx context.Context, userID uuid.UUID) (int, error)
	Activate(ctx context.Context, peerID uuid.UUID, assignedIP string) error
	UpdateStatus(ctx context.Context, peerID uuid.UUID, status string) error
	UpdatePublicKey(ctx context.Context, peerID uuid.UUID, newPubKey string) error
	UpdateLastSeen(ctx context.Context, peerID uuid.UUID, lastSeen time.Time) error
	List(ctx context.Context, params PeerListParams) (*PeerListResult, error)
	CountByStatus(ctx context.Context) (map[string]int, error)
	ListByUserID(ctx context.Context, userID uuid.UUID, params PeerListByUserParams) (*PeerListResult, error)
}

type PeerListParams struct {
	Page      int
	PageSize  int
	Status    string
	StudentID string
	PublicKey string
	SortBy    string
	SortOrder string
}

type PeerListByUserParams struct {
	Page     int
	PageSize int
	Status   string
}

type PeerListResult struct {
	Items []*PeerWithStudent
	Total int
}

type PeerWithStudent struct {
	*model.Peer
	StudentID string `json:"student_id"`
}

type peerStore struct {
	pool *pgxpool.Pool
}

func NewPeerStore(pool *pgxpool.Pool) PeerStore {
	return &peerStore{pool: pool}
}

func (s *peerStore) Create(ctx context.Context, peer *model.Peer) error {
	query := `
		INSERT INTO peers (user_id, public_key, assigned_ip, status, last_seen)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at`
	err := s.pool.QueryRow(ctx, query,
		peer.UserID, peer.PublicKey, peer.AssignedIP, peer.Status, peer.LastSeen,
	).Scan(&peer.ID, &peer.CreatedAt, &peer.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create peer: %w", err)
	}
	return nil
}

func (s *peerStore) FindActiveByUserID(ctx context.Context, userID uuid.UUID) ([]*model.Peer, error) {
	query := `
		SELECT id, user_id, public_key, host(assigned_ip), status, last_seen, created_at, updated_at
		FROM peers WHERE user_id = $1 AND status = 'active'`
	rows, err := s.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("find active peers by user_id: %w", err)
	}
	defer rows.Close()

	var peers []*model.Peer
	for rows.Next() {
		var peer model.Peer
		if err := rows.Scan(
			&peer.ID, &peer.UserID, &peer.PublicKey, &peer.AssignedIP,
			&peer.Status, &peer.LastSeen, &peer.CreatedAt, &peer.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan peer: %w", err)
		}
		peers = append(peers, &peer)
	}
	return peers, nil
}

func (s *peerStore) FindByPublicKey(ctx context.Context, publicKey string) (*model.Peer, error) {
	query := `
		SELECT id, user_id, public_key, host(assigned_ip), status, last_seen, created_at, updated_at
		FROM peers WHERE public_key = $1`
	var peer model.Peer
	err := s.pool.QueryRow(ctx, query, publicKey).Scan(
		&peer.ID, &peer.UserID, &peer.PublicKey, &peer.AssignedIP,
		&peer.Status, &peer.LastSeen, &peer.CreatedAt, &peer.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("find peer by public key: %w", err)
	}
	return &peer, nil
}

func (s *peerStore) FindByID(ctx context.Context, id uuid.UUID) (*model.Peer, error) {
	query := `
		SELECT id, user_id, public_key, host(assigned_ip), status, last_seen, created_at, updated_at
		FROM peers WHERE id = $1`
	var peer model.Peer
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&peer.ID, &peer.UserID, &peer.PublicKey, &peer.AssignedIP,
		&peer.Status, &peer.LastSeen, &peer.CreatedAt, &peer.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("find peer by id: %w", err)
	}
	return &peer, nil
}

func (s *peerStore) ListActive(ctx context.Context) ([]*model.Peer, error) {
	query := `
		SELECT id, user_id, public_key, host(assigned_ip), status, last_seen, created_at, updated_at
		FROM peers WHERE status = 'active'`
	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list active peers: %w", err)
	}
	defer rows.Close()

	var peers []*model.Peer
	for rows.Next() {
		var peer model.Peer
		if err := rows.Scan(
			&peer.ID, &peer.UserID, &peer.PublicKey, &peer.AssignedIP,
			&peer.Status, &peer.LastSeen, &peer.CreatedAt, &peer.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan peer: %w", err)
		}
		peers = append(peers, &peer)
	}
	return peers, nil
}

func (s *peerStore) CountActiveByUserID(ctx context.Context, userID uuid.UUID) (int, error) {
	query := `SELECT COUNT(*) FROM peers WHERE user_id = $1 AND status = 'active'`
	var count int
	if err := s.pool.QueryRow(ctx, query, userID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active peers by user_id: %w", err)
	}
	return count, nil
}

func (s *peerStore) Activate(ctx context.Context, peerID uuid.UUID, assignedIP string) error {
	query := `UPDATE peers SET assigned_ip = $1, status = 'active', updated_at = NOW() WHERE id = $2`
	_, err := s.pool.Exec(ctx, query, assignedIP, peerID)
	if err != nil {
		return fmt.Errorf("activate peer: %w", err)
	}
	return nil
}

func (s *peerStore) UpdateStatus(ctx context.Context, peerID uuid.UUID, status string) error {
	query := `UPDATE peers SET status = $1, updated_at = NOW() WHERE id = $2`
	_, err := s.pool.Exec(ctx, query, status, peerID)
	if err != nil {
		return fmt.Errorf("update peer status: %w", err)
	}
	return nil
}

func (s *peerStore) UpdatePublicKey(ctx context.Context, peerID uuid.UUID, newPubKey string) error {
	query := `UPDATE peers SET public_key = $1, updated_at = NOW() WHERE id = $2`
	_, err := s.pool.Exec(ctx, query, newPubKey, peerID)
	if err != nil {
		return fmt.Errorf("update peer public key: %w", err)
	}
	return nil
}

func (s *peerStore) UpdateLastSeen(ctx context.Context, peerID uuid.UUID, lastSeen time.Time) error {
	query := `UPDATE peers SET last_seen = $1, updated_at = NOW() WHERE id = $2`
	if _, err := s.pool.Exec(ctx, query, lastSeen, peerID); err != nil {
		return fmt.Errorf("update peer last_seen: %w", err)
	}
	return nil
}

func (s *peerStore) List(ctx context.Context, params PeerListParams) (*PeerListResult, error) {
	var conditions []string
	var args []interface{}
	argIdx := 1

	if params.Status != "" {
		conditions = append(conditions, fmt.Sprintf("p.status = $%d", argIdx))
		args = append(args, params.Status)
		argIdx++
	}
	if params.StudentID != "" {
		conditions = append(conditions, fmt.Sprintf("u.student_id LIKE $%d", argIdx))
		args = append(args, "%"+params.StudentID+"%")
		argIdx++
	}
	if params.PublicKey != "" {
		conditions = append(conditions, fmt.Sprintf("p.public_key LIKE $%d", argIdx))
		args = append(args, "%"+params.PublicKey+"%")
		argIdx++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	sortBy := "p.created_at"
	if params.SortBy == "status" || params.SortBy == "public_key" || params.SortBy == "assigned_ip" {
		sortBy = "p." + params.SortBy
	}
	sortOrder := "DESC"
	if params.SortOrder == "ASC" {
		sortOrder = "ASC"
	}

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM peers p JOIN users u ON p.user_id = u.id %s`, whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count peers: %w", err)
	}

	offset := (params.Page - 1) * params.PageSize
	listQuery := fmt.Sprintf(
		`SELECT p.id, p.user_id, p.public_key, host(p.assigned_ip), p.status, p.last_seen, p.created_at, p.updated_at, u.student_id
		FROM peers p JOIN users u ON p.user_id = u.id %s ORDER BY %s %s LIMIT $%d OFFSET $%d`,
		whereClause, sortBy, sortOrder, argIdx, argIdx+1,
	)
	args = append(args, params.PageSize, offset)

	rows, err := s.pool.Query(ctx, listQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("list peers: %w", err)
	}
	defer rows.Close()

	var items []*PeerWithStudent
	for rows.Next() {
		var item PeerWithStudent
		var peer model.Peer
		if err := rows.Scan(
			&peer.ID, &peer.UserID, &peer.PublicKey, &peer.AssignedIP,
			&peer.Status, &peer.LastSeen, &peer.CreatedAt, &peer.UpdatedAt,
			&item.StudentID,
		); err != nil {
			return nil, fmt.Errorf("scan peer: %w", err)
		}
		item.Peer = &peer
		items = append(items, &item)
	}

	return &PeerListResult{Items: items, Total: total}, nil
}

func (s *peerStore) CountByStatus(ctx context.Context) (map[string]int, error) {
	query := `SELECT status, COUNT(*) FROM peers GROUP BY status`
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

	for _, key := range []string{"active", "disconnected", "revoked"} {
		if _, ok := result[key]; !ok {
			result[key] = 0
		}
	}

	return result, nil
}

func (s *peerStore) ListByUserID(ctx context.Context, userID uuid.UUID, params PeerListByUserParams) (*PeerListResult, error) {
	var conditions []string
	var args []interface{}
	argIdx := 1

	conditions = append(conditions, fmt.Sprintf("p.user_id = $%d", argIdx))
	args = append(args, userID)
	argIdx++

	if params.Status != "" {
		conditions = append(conditions, fmt.Sprintf("p.status = $%d", argIdx))
		args = append(args, params.Status)
		argIdx++
	}

	whereClause := "WHERE " + strings.Join(conditions, " AND ")

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM peers p %s`, whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count peers by user_id: %w", err)
	}

	offset := (params.Page - 1) * params.PageSize
	listQuery := fmt.Sprintf(
		`SELECT p.id, p.user_id, p.public_key, host(p.assigned_ip), p.status, p.last_seen, p.created_at, p.updated_at, u.student_id
		FROM peers p JOIN users u ON p.user_id = u.id %s ORDER BY p.created_at DESC LIMIT $%d OFFSET $%d`,
		whereClause, argIdx, argIdx+1,
	)
	args = append(args, params.PageSize, offset)

	rows, err := s.pool.Query(ctx, listQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("list peers by user_id: %w", err)
	}
	defer rows.Close()

	var items []*PeerWithStudent
	for rows.Next() {
		var item PeerWithStudent
		var peer model.Peer
		if err := rows.Scan(
			&peer.ID, &peer.UserID, &peer.PublicKey, &peer.AssignedIP,
			&peer.Status, &peer.LastSeen, &peer.CreatedAt, &peer.UpdatedAt,
			&item.StudentID,
		); err != nil {
			return nil, fmt.Errorf("scan peer: %w", err)
		}
		item.Peer = &peer
		items = append(items, &item)
	}

	return &PeerListResult{Items: items, Total: total}, nil
}
