package indexdb

type Option func(*Store)

func WithDimentions(d int) Option {
	return func(s *Store) {
		s.dimentions = d
	}
}
