package service

import "time"

// WaitSourcesArchive blocks until every background job SourcesArchive started
// has finished.
func (s *Service) WaitSourcesArchive() { s.archiveJobs.Wait() }

// SetSourcesArchiveClock replaces the clock SourcesArchive's memory of recent
// saves reads. Call it before SourcesArchive.
func (s *Service) SetSourcesArchiveClock(now func() time.Time) { s.recentArchives.now = now }
