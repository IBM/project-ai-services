---
name: Bug report
about: Create a report to help us improve project-ai-services
title: ''
labels: bug

---

## 🐛 Description
<!-- A clear and concise description of what the bug is. -->
A clear and detailed description of the issue.

## ✅ Expected Behavior
<!-- A clear and concise description of what you expected to happen. -->
What should have happened?

## ❌ Actual Behavior
<!-- A clear and concise description of what actually happened -->
What actually happened?

## 🔁 Steps to Reproduce

Steps to reproduce the behavior:

1.
2.
3.

## 🖥️ Environment Info

- RHEL Version: [output of `cat /etc/redhat-release`]
- GoLang Version: [output of `go version`]
- AI-services Version: [output of `ai-services version`]
- If not using RHEL - add OS and version details here

## 🧪 Diagnostic Commands & Output

Please run the following commands and paste their output:

```bash
ai-services bootstrap configure
ai-services bootstrap validate
ai-services application ps -o wide
```

## 📸 Screenshots / Logs
<!-- If applicable, add screenshots to help explain your problem. -->
Attach pod logs or screenshots if available.
If reporting issue for an unhealthy/mis-behaving pod, attach logs for specific pod(s)
```bash
ai-services application logs --pod <podName>
```


## 📎 Additional Context
<!-- Add any other context about the problem here. -->