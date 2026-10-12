package repository

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/jackc/pgx/v5"
)

var consolePlanHash = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ConsoleDatasetDescriptor exposes no CSV, private evidence or raw principal
// record. A caller may NOT receive it without independent API authorisation.
type ConsoleDatasetDescriptor struct {
	Intent    domain.ConsoleDatasetIntent
	State     string
	CheckerID string
}

func (r *PostgresRepository) GetConsoleDatasetDescriptor(ctx context.Context, digest string) (ConsoleDatasetDescriptor, error) {
	var out ConsoleDatasetDescriptor
	if !consolePlanHash.MatchString(digest) {
		return out, ErrConsoleDatasetDenied
	}
	var intentJSON []byte
	err := r.pool.QueryRow(ctx, `SELECT intent,state,COALESCE(checker,'') FROM operations.console_dataset_plan WHERE digest=$1`, digest).Scan(&intentJSON, &out.State, &out.CheckerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrConsoleDatasetDenied
	}
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(intentJSON, &out.Intent); err != nil {
		return ConsoleDatasetDescriptor{}, err
	}
	return out, nil
}
