#!/usr/bin/env python3
"""
Validates that Makefile TAG versions have been bumped when component files change.

When files in a component directory are modified, the corresponding Makefile TAG
must be incremented to ensure proper versioning and deployment.
"""

import re
import subprocess
import sys
from pathlib import Path
from typing import List, Optional, Tuple

# Map of component paths to their configuration
# Format: (component_path, name)
COMPONENTS = [
    # Services Images
    ("services/chatbot", "chatbot-service"),
    ("services/digitize", "digitize-service"),
    ("services/similarity", "similarity-service"),
    ("services/summarize", "summarize-service"),
    # Images
    ("images/service-base", "service-base"),
    ("images/postgres", "postgres"),
    ("images/caddy", "caddy"),
    ("images/litellm", "litellm"),
    ("images/tools", "tools"),
    # UI Images
    ("ui/chatbot", "chatbot-ui"),
    ("ui/digitize", "digitize-ui"),
    ("ui/catalog", "catalog-ui"),
    # Ai Services
    ("ai-services", "ai-services"),
]


def get_changed_files(base_ref: str) -> List[str]:
    """Get list of changed files compared to base branch."""
    try:
        result = subprocess.run(
            ["git", "diff", "--name-only", f"origin/{base_ref}...HEAD"],
            capture_output=True,
            text=True,
            check=True,
        )
        return [line.strip() for line in result.stdout.strip().split("\n") if line.strip()]
    except subprocess.CalledProcessError as e:
        print(f"❌ Error getting changed files: {e}")
        sys.exit(1)


def get_makefile_tag(makefile_path: Path, ref: Optional[str] = None, componentPath: Optional[str] = None) -> Optional[str]:
    """
    Extract TAG value from a Makefile, calculating it if it references other variables.

    Args:
        makefile_path: Path to the Makefile
        ref: Git ref to read from (e.g., 'origin/main'). If None, reads from working tree.
        componentPath: Component path (needed for git operations)

    Returns:
        TAG value or None if not found
    """
    try:
        if ref:
            # Read from git ref
            result = subprocess.run(
                ["git", "show", f"{ref}:./{componentPath}/Makefile"],
                capture_output=True,
                text=True,
                check=True,
            )
            content = result.stdout
        else:
            # Read from working tree
            content = makefile_path.read_text()

        # Extract all variable definitions
        variables = {}
        for line in content.split('\n'):
            # Match variable assignments: VAR?=value or VAR=value
            var_match = re.match(
                r'^(\w+)\s*\??\s*=\s*(.+?)(?:\s*#.*)?$', line.strip())
            if var_match:
                var_name = var_match.group(1)
                var_value = var_match.group(2).strip()
                variables[var_name] = var_value

        # Get TAG value
        tag_value = variables.get('TAG')
        if not tag_value:
            return None

        # If TAG references other variables, resolve them
        # Handle patterns like: $(VAR1)-$(VAR2) or v$(VAR1)-$(VAR2)
        def resolve_variables(value: str) -> str:
            # Replace $(VAR) with actual values
            pattern = r'\$\((\w+)\)'
            while re.search(pattern, value):
                match = re.search(pattern, value)
                if match:
                    var_name = match.group(1)
                    var_replacement = variables.get(var_name, match.group(0))
                    value = value.replace(match.group(0), var_replacement)
            return value

        resolved_tag = resolve_variables(tag_value)
        return resolved_tag

    except subprocess.CalledProcessError as e:
        # File doesn't exist in the ref or git error
        stderr = e.stderr.strip() if e.stderr else "unknown error"
        print(
            f"   ⚠️  Warning: Could not read {makefile_path} from {ref}: {stderr}")
        return None
    except FileNotFoundError:
        return None


def check_component_version_bump(
    component_path: str,
    name: str,
    changed_files: List[str],
    base_ref: str,
    repo_root: Path,
) -> Tuple[bool, Optional[str]]:
    """
    Check if a component's Makefile TAG has been bumped.

    Returns:
        (needs_check, error_message) tuple
        - needs_check: True if component has changes and needs version check
        - error_message: Error message if TAG wasn't bumped, None otherwise
    """
    # Check if any files in this component changed
    component_changed = any(
        f.startswith(f"{component_path}/") for f in changed_files
    )

    if not component_changed:
        return False, None

    makefile_path = repo_root / component_path / "Makefile"

    if not makefile_path.exists():
        return True, f"❌ Makefile not found: {component_path}/Makefile"

    # Get TAG from base branch
    base_tag = get_makefile_tag(
        makefile_path, f"origin/{base_ref}", component_path)

    # Get TAG from current branch
    head_tag = get_makefile_tag(makefile_path)

    if base_tag is None:
        return True, f"❌ Could not find TAG in base branch: {component_path}/Makefile"

    if head_tag is None:
        return True, f"❌ Could not find TAG in current branch: {component_path}/Makefile"

    # Check if TAG was bumped
    if base_tag == head_tag:
        error_msg = [
            f"❌ ERROR: The image TAG in {component_path}/Makefile has not been bumped.",
            f"   Component    : {name}",
            f"   Current TAG  : {head_tag}",
            f"   Changes to {component_path}/** require a version bump.",
            f"   Please update TAG in {component_path}/Makefile",
            f"   and update image references in values.yaml files under ai-services/assets/.",
            "",
            "   Run: python3 .github/scripts/check_image_names.py  (after bumping)",
        ]

        return True, "\n".join(error_msg)

    # TAG was bumped successfully
    print(f"✅ {name}: TAG bumped {base_tag} → {head_tag}")
    return True, None


def main() -> int:
    """Main entry point."""
    # Get base branch from environment or default to 'main'
    base_ref = sys.argv[1] if len(sys.argv) > 1 else "main"

    repo_root = Path(__file__).parent.parent.parent

    print("=" * 70)
    print("Checking Makefile version bumps for changed components...")
    print(f"Base branch: {base_ref}")
    print("=" * 70)
    print()

    # Get changed files
    changed_files = get_changed_files(base_ref)

    if not changed_files:
        print("ℹ️  No files changed")
        return 0
    print()

    errors = []
    passed_components = []
    failed_components = []

    # Check each component
    for component_path, name in COMPONENTS:
        needs_check, error_msg = check_component_version_bump(
            component_path, name, changed_files, base_ref, repo_root
        )

        if needs_check:
            if error_msg:
                errors.append(error_msg)
                failed_components.append(name)
            else:
                passed_components.append(name)

    print()

    if not passed_components and not failed_components:
        print("ℹ️  No components with changes detected")
        return 0

    # Display summary
    print("=" * 70)
    print("SUMMARY")
    print("=" * 70)
    print()

    if passed_components:
        print(f"✅ PASSED ({len(passed_components)}):")
        for name in passed_components:
            print(f"   • {name}")
        print()

    if failed_components:
        print(f"❌ FAILED ({len(failed_components)}):")
        for name in failed_components:
            print(f"   • {name}")
        print()

    if errors:
        print("=" * 70)
        print("DETAILED ERRORS")
        print("=" * 70)
        print()
        for err in errors:
            print(err)
            print()
        print(
            "When updating component files, you must bump the TAG in the\n"
            "corresponding Makefile and update image references in values.yaml files."
        )
        return 1

    print("=" * 70)
    print(
        f"✅ All {len(passed_components)} component(s) have proper version bumps!")
    print("=" * 70)
    return 0


if __name__ == "__main__":
    sys.exit(main())

# Made with Bob
