# Claude Code Guidelines

## Code Comments

Only comment on non-obvious code segments. Avoid comments that simply restate what the code does when it's already clear from the function/variable names.

**Bad examples:**
```go
// Check if job failed
if isJobFailed(job) {

// Job is still running
return ...
```

**Good examples:**
```go
// RunAsUser 0 (root) is needed to read /proc/<pid>/environ of other users' processes
// and to access /proc/<pid>/root when using shared PID namespace.
SecurityContext: &corev1.SecurityContext{
    RunAsUser: ptr.To(int64(0)),
}
```

If code needs a comment to be understood, first consider if better naming or restructuring could make it self-explanatory.
