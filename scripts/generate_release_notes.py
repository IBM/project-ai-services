#!/usr/bin/env python3
"""
Release Notes Generator

Generates release notes between Git tags by collecting commits
and creating a formatted markdown file.

Usage:
    ./scripts/generate_release_notes.py <version>

Example:
    ./scripts/generate_release_notes.py v0.2.0
"""

import sys
import subprocess
import os
from datetime import datetime
from pathlib import Path


def run_git_command(command):
    """Execute a git command and return the output."""
    try:
        result = subprocess.run(
            command,
            shell=True,
            check=True,
            capture_output=True,
            text=True
        )
        return result.stdout.strip()
    except subprocess.CalledProcessError as e:
        print(f"Error executing git command: {e}", file=sys.stderr)
        print(f"Command output: {e.stderr}", file=sys.stderr)
        sys.exit(1)


def get_previous_tag():
    """Get the most recent git tag."""
    tags = run_git_command("git tag --sort=-version:refname")
    if not tags:
        return None
    return tags.split('\n')[0]


def get_commits_between(previous_tag, current_ref="HEAD"):
    """Get commits between previous tag and current reference."""
    if previous_tag:
        git_range = f"{previous_tag}..{current_ref}"
    else:
        # If no previous tag, get all commits
        git_range = current_ref
    
    # Get commit log with format: short_sha|commit_message
    commits_output = run_git_command(
        f'git log {git_range} --pretty=format:"%h|%s"'
    )
    
    if not commits_output:
        return []
    
    commits = []
    for line in commits_output.split('\n'):
        if '|' in line:
            sha, message = line.split('|', 1)
            commits.append({'sha': sha, 'message': message})
    
    return commits


def generate_summary(commits):
    """Generate a natural language summary from commit messages."""
    if not commits:
        return "No changes in this release."
    
    # Collect all commit messages
    messages = [commit['message'] for commit in commits]
    
    # Simple categorization
    features = []
    fixes = []
    improvements = []
    others = []
    
    for msg in messages:
        msg_lower = msg.lower()
        if any(keyword in msg_lower for keyword in ['add', 'new', 'feature', 'implement']):
            features.append(msg)
        elif any(keyword in msg_lower for keyword in ['fix', 'bug', 'resolve', 'correct']):
            fixes.append(msg)
        elif any(keyword in msg_lower for keyword in ['improve', 'update', 'enhance', 'refactor', 'optimize']):
            improvements.append(msg)
        else:
            others.append(msg)
    
    # Build summary
    summary_parts = []
    
    if features:
        summary_parts.append(f"This release includes {len(features)} new feature(s)")
    if fixes:
        summary_parts.append(f"{len(fixes)} bug fix(es)")
    if improvements:
        summary_parts.append(f"{len(improvements)} improvement(s)")
    if others and not (features or fixes or improvements):
        summary_parts.append(f"{len(others)} change(s)")
    
    if not summary_parts:
        return f"This release contains {len(commits)} commit(s) with various updates and changes."
    
    summary = "This release includes " + ", ".join(summary_parts) + "."
    
    # Add specific highlights
    highlights = []
    if features:
        highlights.append(f"New features include: {features[0]}")
    if fixes:
        highlights.append(f"Key fixes: {fixes[0]}")
    
    if highlights:
        summary += " " + " ".join(highlights) + "."
    
    return summary


def create_release_notes(version, previous_tag, commits):
    """Create the release notes markdown content."""
    date = datetime.now().strftime("%Y-%m-%d")
    
    # Generate summary
    summary = generate_summary(commits)
    
    # Build markdown content
    content = f"# Release {version}\n"
    content += f"Date: {date}\n\n"
    content += "## Summary\n"
    content += f"{summary}\n\n"
    content += "## Commits\n"
    
    if commits:
        for commit in commits:
            content += f"- {commit['message']} ({commit['sha']})\n"
    else:
        content += "- No commits in this release\n"
    
    return content


def main():
    """Main function to generate release notes."""
    if len(sys.argv) != 2:
        print("Usage: ./scripts/generate_release_notes.py <version>", file=sys.stderr)
        print("Example: ./scripts/generate_release_notes.py v0.2.0", file=sys.stderr)
        sys.exit(1)
    
    version = sys.argv[1]
    
    # Ensure we're in a git repository
    try:
        run_git_command("git rev-parse --git-dir")
    except SystemExit:
        print("Error: Not in a git repository", file=sys.stderr)
        sys.exit(1)
    
    # Create changelog directory if it doesn't exist
    changelog_dir = Path("changelog")
    changelog_dir.mkdir(exist_ok=True)
    
    # Check if release notes already exist
    release_file = changelog_dir / f"{version}.md"
    if release_file.exists():
        print(f"Error: Release notes for {version} already exist at {release_file}", file=sys.stderr)
        sys.exit(1)
    
    # Get previous tag
    previous_tag = get_previous_tag()
    
    if previous_tag:
        print(f"Generating release notes from {previous_tag} to HEAD...")
    else:
        print("No previous tags found. Generating release notes from all commits...")
    
    # Get commits
    commits = get_commits_between(previous_tag)
    
    if not commits:
        print(f"Error: No commits found between {previous_tag or 'start'} and HEAD", file=sys.stderr)
        sys.exit(1)
    
    print(f"Found {len(commits)} commit(s)")
    
    # Generate release notes
    content = create_release_notes(version, previous_tag, commits)
    
    # Write to file
    release_file.write_text(content)
    
    print(f"✓ Release notes created: {release_file}")
    print(f"\nNext steps:")
    print(f"  1. Review the generated release notes: {release_file}")
    print(f"  2. Create the git tag: git tag {version}")
    print(f"  3. Add and commit the release notes:")
    print(f"     git add {release_file}")
    print(f"     git commit -m \"Add release notes for {version}\"")
    print(f"  4. Push the tag: git push origin {version}")


if __name__ == "__main__":
    main()

# Made with Bob
