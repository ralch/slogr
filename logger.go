package slogr

import "golang.org/x/exp/slog"

var _ slog.Leveler = LevelVar("")

// StringLevel represents a slog.Leveler for string
type LevelVar string

// Set set the value.
func (v *LevelVar) Set(value string) {
  *v = LevelVar(value)
}

// String returns the level as string.
func (v LevelVar) String() string {
	return string(v)
}

// Level implements [slog.Leveler].
func (v LevelVar) Level() slog.Level {
	data := []byte(v)

	var level slog.Level
	// unmarshal the level
	_ = level.UnmarshalText(data)
	// done!
	return level
}

// MarshalText implements [encoding.TextMarshaler] by calling [Level.MarshalText].
func (v *LevelVar) MarshalText() ([]byte, error) {
	return v.Level().MarshalText()
}

// UnmarshalText implements [encoding.TextUnmarshaler] by calling [Level.UnmarshalText].
func (v *LevelVar) UnmarshalText(data []byte) error {
	var level slog.Level

	if err := level.UnmarshalText(data); err != nil {
		return err
	}

  *v = LevelVar(level.String())
	return nil
}
