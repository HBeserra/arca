package indexdb

import "log/slog"

type Option func(*Store)

func WithDimensions(d int) Option {
	return func(s *Store) {
		s.dimensions = d
	}
}

func WithLogger(log *slog.Logger) Option {
	return func(s *Store) {
		s.log = log
	}
}
