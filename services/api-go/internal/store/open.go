package store

import "context"

func Open(ctx context.Context, databaseURL string) (Repository, func(), error) {
	if databaseURL == "" {
		return NewMemory(), func() {}, nil
	}
	repo, err := NewPostgres(ctx, databaseURL)
	if err != nil {
		return nil, nil, err
	}
	return repo, repo.Close, nil
}
