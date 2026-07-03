package main

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// CronJob represents a cron job entry
type CronJob struct {
	ID       string `json:"id"`
	Schedule string `json:"schedule"` // "* * * * *" format
	Command  string `json:"command"`
	Enabled  bool   `json:"enabled"`
	Comment  string `json:"comment,omitempty"`
}

// ListCronJobsRequest for listing cron jobs
type ListCronJobsRequest struct {
	Username string `json:"username"` // system user (e.g., www-data, domain user)
}

// ListCronJobsResponse contains cron job list
type ListCronJobsResponse struct {
	Jobs []CronJob `json:"jobs"`
}

// AddCronJobRequest for adding a new cron job
type AddCronJobRequest struct {
	Username string `json:"username"`
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
	Comment  string `json:"comment,omitempty"`
}

// UpdateCronJobRequest for updating an existing cron job
type UpdateCronJobRequest struct {
	Username string `json:"username"`
	ID       string `json:"id"`
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
	Enabled  bool   `json:"enabled"`
	Comment  string `json:"comment,omitempty"`
}

// DeleteCronJobRequest for deleting a cron job
type DeleteCronJobRequest struct {
	Username string `json:"username"`
	ID       string `json:"id"`
}

// ListCronJobs lists all cron jobs for a user
func (a *Agent) ListCronJobs(req *ListCronJobsRequest, resp *ListCronJobsResponse) error {
	username := req.Username
	if username == "" {
		username = "www-data"
	}

	// Read crontab for user
	cmd := exec.Command("crontab", "-u", username, "-l")
	output, err := cmd.Output()
	if err != nil {
		// No crontab for user is not an error
		if strings.Contains(err.Error(), "no crontab") {
			resp.Jobs = []CronJob{}
			return nil
		}
		resp.Jobs = []CronJob{}
		return nil
	}

	resp.Jobs = parseCrontab(string(output))
	return nil
}

// AddCronJob adds a new cron job
func (a *Agent) AddCronJob(req *AddCronJobRequest, resp *bool) error {
	username := req.Username
	if username == "" {
		username = "www-data"
	}

	// Validate schedule
	if !isValidCronSchedule(req.Schedule) {
		return fmt.Errorf("invalid cron schedule: %s", req.Schedule)
	}

	// Get existing crontab
	existing := getCrontab(username)

	// Build new entry
	var newEntry string
	if req.Comment != "" {
		newEntry = fmt.Sprintf("# %s\n%s %s", req.Comment, req.Schedule, req.Command)
	} else {
		newEntry = fmt.Sprintf("%s %s", req.Schedule, req.Command)
	}

	// Append new entry
	newCrontab := existing
	if newCrontab != "" && !strings.HasSuffix(newCrontab, "\n") {
		newCrontab += "\n"
	}
	newCrontab += newEntry + "\n"

	// Write new crontab
	if err := setCrontab(username, newCrontab); err != nil {
		return err
	}

	*resp = true
	return nil
}

// UpdateCronJob updates an existing cron job
func (a *Agent) UpdateCronJob(req *UpdateCronJobRequest, resp *bool) error {
	username := req.Username
	if username == "" {
		username = "www-data"
	}

	// Validate schedule
	if !isValidCronSchedule(req.Schedule) {
		return fmt.Errorf("invalid cron schedule: %s", req.Schedule)
	}

	// Get existing crontab
	existing := getCrontab(username)
	lines := strings.Split(existing, "\n")

	// Find and update the job
	var newLines []string
	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			newLines = append(newLines, line)
			continue
		}

		// Generate ID for this line
		lineID := generateCronID(trimmed)
		if lineID == req.ID {
			found = true
			// Replace with updated job
			var newLine string
			if !req.Enabled {
				newLine = "# DISABLED: " + req.Schedule + " " + req.Command
			} else {
				newLine = req.Schedule + " " + req.Command
			}
			
			// Add comment if provided
			if req.Comment != "" && (i == 0 || !strings.HasPrefix(strings.TrimSpace(lines[i-1]), "#")) {
				newLines = append(newLines, "# "+req.Comment)
			}
			newLines = append(newLines, newLine)
		} else {
			newLines = append(newLines, line)
		}
	}

	if !found {
		return fmt.Errorf("cron job not found: %s", req.ID)
	}

	// Write new crontab
	if err := setCrontab(username, strings.Join(newLines, "\n")); err != nil {
		return err
	}

	*resp = true
	return nil
}

// DeleteCronJob deletes a cron job
func (a *Agent) DeleteCronJob(req *DeleteCronJobRequest, resp *bool) error {
	username := req.Username
	if username == "" {
		username = "www-data"
	}

	// Get existing crontab
	existing := getCrontab(username)
	lines := strings.Split(existing, "\n")

	// Filter out the job to delete
	var newLines []string
	found := false
	skipNextComment := false

	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			continue
		}

		// Check if this is the job to delete
		if !strings.HasPrefix(trimmed, "#") {
			lineID := generateCronID(trimmed)
			if lineID == req.ID {
				found = true
				skipNextComment = true
				continue
			}
		}

		// Skip comment line before deleted job
		if skipNextComment && strings.HasPrefix(trimmed, "#") {
			skipNextComment = false
			continue
		}

		skipNextComment = false
		newLines = append([]string{line}, newLines...)
	}

	if !found {
		return fmt.Errorf("cron job not found: %s", req.ID)
	}

	// Write new crontab
	if err := setCrontab(username, strings.Join(newLines, "\n")); err != nil {
		return err
	}

	*resp = true
	return nil
}

// Helper functions

func parseCrontab(content string) []CronJob {
	var jobs []CronJob
	lines := strings.Split(content, "\n")
	var currentComment string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Check for comment
		if strings.HasPrefix(trimmed, "#") {
			// Check if it's a disabled job
			if strings.HasPrefix(trimmed, "# DISABLED:") {
				// Parse disabled job
				disabled := strings.TrimPrefix(trimmed, "# DISABLED:")
				parts := parseCronLine(strings.TrimSpace(disabled))
				if parts != nil {
					jobs = append(jobs, CronJob{
						ID:       generateCronID(strings.TrimSpace(disabled)),
						Schedule: parts[0],
						Command:  parts[1],
						Enabled:  false,
						Comment:  currentComment,
					})
				}
				currentComment = ""
			} else {
				currentComment = strings.TrimPrefix(trimmed, "# ")
			}
			continue
		}

		// Parse active job
		parts := parseCronLine(trimmed)
		if parts != nil {
			jobs = append(jobs, CronJob{
				ID:       generateCronID(trimmed),
				Schedule: parts[0],
				Command:  parts[1],
				Enabled:  true,
				Comment:  currentComment,
			})
		}
		currentComment = ""
	}

	return jobs
}

func parseCronLine(line string) []string {
	// Cron format: min hour dom month dow command
	// Need to extract first 5 fields and the rest is command
	fields := strings.Fields(line)
	if len(fields) < 6 {
		return nil
	}

	schedule := strings.Join(fields[:5], " ")
	command := strings.Join(fields[5:], " ")

	return []string{schedule, command}
}

func generateCronID(line string) string {
	// Simple content hash over bytes; iterating bytes keeps the uint32
	// conversion in range (a rune could exceed it in theory).
	// Baytlar üzerinden basit içerik özeti; baytları dolaşmak uint32
	// dönüşümünü aralıkta tutar (bir rune teoride bunu aşabilir).
	var hash uint32
	for _, c := range []byte(line) {
		hash = hash*31 + uint32(c)
	}
	return fmt.Sprintf("%08x", hash)
}

func isValidCronSchedule(schedule string) bool {
	fields := strings.Fields(schedule)
	if len(fields) != 5 {
		return false
	}

	// Basic validation - each field should be valid cron expression
	cronFieldPattern := regexp.MustCompile(`^(\*|[0-9]+(-[0-9]+)?(,[0-9]+(-[0-9]+)?)*|(\*/[0-9]+))$`)
	for _, field := range fields {
		if !cronFieldPattern.MatchString(field) {
			return false
		}
	}

	return true
}

func getCrontab(username string) string {
	cmd := exec.Command("crontab", "-u", username, "-l")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(output)
}

func setCrontab(username, content string) error {
	cmd := exec.Command("crontab", "-u", username, "-")
	cmd.Stdin = strings.NewReader(content)
	return cmd.Run()
}
