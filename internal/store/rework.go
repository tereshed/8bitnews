package store

import (
	"context"
	"errors"
	"time"
)

type ReworkJob struct {
	ID, ReleaseID, TaskID, Note, LeaseToken string
	Attempts                                int
}

func (s *Store) ClaimRework(ctx context.Context) (ReworkJob, bool, error) {
	_, err := s.pool.Exec(ctx, `UPDATE rework_jobs SET status='needs_human', last_error='Dispatcher lease expired after five attempts', updated_at=now()
		WHERE status='dispatching' AND lease_until<now() AND attempts>=5`)
	if err != nil {
		return ReworkJob{}, false, err
	}
	// Releases which have since been withdrawn/rejected no longer need work.
	_, err = s.pool.Exec(ctx, `UPDATE rework_jobs j SET status='cancelled', updated_at=now()
		FROM releases r WHERE r.id=j.release_id AND r.status <> 'changes_requested' AND j.status IN ('pending','dispatching')`)
	if err != nil {
		return ReworkJob{}, false, err
	}
	var job ReworkJob
	err = s.pool.QueryRow(ctx, `UPDATE rework_jobs SET status='dispatching', attempts=attempts+1,
		lease_token=gen_random_uuid(), lease_until=now()+interval '2 minutes', updated_at=now()
		WHERE id=(SELECT id FROM rework_jobs WHERE
			(status='pending' AND available_at<=now()) OR (status='dispatching' AND lease_until<now())
			ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING id, release_id, task_id, note, lease_token, attempts`).Scan(&job.ID, &job.ReleaseID, &job.TaskID, &job.Note, &job.LeaseToken, &job.Attempts)
	if errors.Is(mapNotFound(err), ErrNotFound) {
		return ReworkJob{}, false, nil
	}
	return job, err == nil, err
}

func (s *Store) FinishRework(ctx context.Context, job ReworkJob, failure string, permanent bool) error {
	status := "sent"
	if failure != "" {
		status = "pending"
		if permanent || job.Attempts >= 5 {
			status = "needs_human"
		}
	}
	delay := min(time.Duration(job.Attempts)*time.Minute, 5*time.Minute)
	_, err := s.pool.Exec(ctx, `UPDATE rework_jobs SET status=$3, last_error=$4, available_at=now()+make_interval(secs=>$5),
		lease_token=NULL, lease_until=NULL, updated_at=now() WHERE id=$1 AND lease_token=$2 AND status='dispatching'`,
		job.ID, job.LeaseToken, status, failure, delay.Seconds())
	return err
}
