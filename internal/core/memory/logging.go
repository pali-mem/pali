package memory

import (
	"strings"
)

func (s *Service) logInfof(format string, args ...any) {
	if s == nil || s.logger == nil {
		return
	}
	s.logger.Printf(format, args...)
}

func (s *Service) logDebugf(format string, args ...any) {
	if s == nil || s.logger == nil || !s.devVerbose {
		return
	}
	s.logger.Printf(format, args...)
}

func sanitizeLogSnippet(in string, max int) string {
	in = strings.Join(strings.Fields(strings.TrimSpace(in)), " ")
	if max <= 0 || len(in) <= max {
		return in
	}
	if max <= 3 {
		return in[:max]
	}
	return in[:max-3] + "..."
}
