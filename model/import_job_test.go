package model

import (
	"testing"

	"emailtracker.com/db"
)

func TestEnqueueAndFinishImportJob(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser("import-job@test.com", "hash", "http://localhost")
	job, err := EnqueueImportJob(userID, ImportKindContactsPaste, ImportJobPayload{
		Rows: []ImportContactRow{{Email: "a@test.com"}, {Email: "b@test.com"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != ImportStatusPending || job.TotalRows != 2 {
		t.Fatalf("job=%+v", job)
	}

	claimed, ok, err := ClaimNextImportJob()
	if err != nil || !ok || claimed.ID != job.ID {
		t.Fatalf("claim ok=%v err=%v id=%d", ok, err, claimed.ID)
	}
	result := ImportContactsResult{Created: 2}
	if err := FinishImportJob(claimed.ID, ImportStatusDone, "2 created", "", result, 2); err != nil {
		t.Fatal(err)
	}
	done, err := GetImportJob(claimed.ID)
	if err != nil || done.Status != ImportStatusDone || done.CreatedCount != 2 {
		t.Fatalf("done=%+v err=%v", done, err)
	}
}
