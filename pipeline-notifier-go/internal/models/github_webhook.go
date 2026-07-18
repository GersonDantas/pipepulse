package models

type GithubWebhookPayload struct {
	Repository  GithubRepository  `json:"repository" binding:"required"`
	WorkflowRun GithubWorkflowRun `json:"workflow_run" binding:"required"`
}

type GithubRepository struct {
	ID int64 `json:"id" binding:"required"`
}

type GithubWorkflowRun struct {
	ID         int64   `json:"id" binding:"required"`
	WorkflowID int64   `json:"workflow_id" binding:"required"`
	RunAttempt int     `json:"run_attempt" binding:"required"`
	Status     string  `json:"status" binding:"required"`
	Conclusion *string `json:"conclusion"`
	UpdatedAt  string  `json:"updated_at" binding:"required"`
	HeadBranch string  `json:"head_branch"`
	HeadSHA    string  `json:"head_sha"`
	HTMLURL    string  `json:"html_url"`
}
