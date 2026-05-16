package models

type GithubWebhookPayload struct {
	WorkflowRun GithubWorkflowRun `json:"workflow_run" binding:"required"`
}

type GithubWorkflowRun struct {
	ID         int64   `json:"id" binding:"required"`
	Conclusion *string `json:"conclusion"`
	UpdatedAt  string  `json:"updated_at" binding:"required"`
}
