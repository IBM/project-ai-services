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
# Format: (component_path, values_yaml_key, name)
COMPONENTS = [
    # Services Images
    ("services/chatbot", "backend", "chatbot-service"),
    ("services/digitize", "digitize", "digitize-service"),
    ("services/similarity", "similarity", "similarity-service"),
    ("services/summarize", "summarize", "summarize-service"),
    # Images
    ("images/service-base", "", "service-base"),
    ("images/postgres", "postgres", "postgres"),
    ("images/caddy", "caddy", "caddy"),
    ("images/litellm", "litellm", "litellm"),
    # UI Images
    ("ui/chatbot", "ui", "chatbot-ui"),
    ("ui/digitize", "digitizeUi", "digitize-ui"),
    # Catalog
    ("ui/catalog", "ui", "catalog-ui"),
    ("ai-services", "backend", "ai-services"),
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


def get_makefile_tag(makefile_path: Path, ref: Optional[str] = None) -> Optional[str]:
    """
    Extract TAG value from a Makefile.
    
    Args:
        makefile_path: Path to the Makefile
        ref: Git ref to read from (e.g., 'origin/main'). If None, reads from working tree.
    
    Returns:
        TAG value or None if not found
    """
    try:
        if ref:
            # Read from git ref
            result = subprocess.run(
                ["git", "show", f"{ref}:{makefile_path}"],
                capture_output=True,
                text=True,
                check=True,
            )
            content = result.stdout
        else:
            # Read from working tree
            content = makefile_path.read_text()
        
        # Match TAG= or TAG?= with optional whitespace
        tag_match = re.search(r"^TAG\s*\??\s*=\s*(\S+)", content, re.MULTILINE)
        if tag_match:
            return tag_match.group(1)
        return None
    except subprocess.CalledProcessError as e:
        # File doesn't exist in the ref or git error
        print(f"   ⚠️  Warning: Could not read {makefile_path} from {ref}: {e.stderr}")
        return None
    except FileNotFoundError:
        return None


def check_component_version_bump(
    component_path: str,
    values_yaml_key: str,
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
    base_tag = get_makefile_tag(makefile_path, f"origin/{base_ref}")
    
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
        ]
        
        if values_yaml_key:
            error_msg.extend([
                f"   and the corresponding {values_yaml_key}.image references",
                "   in all values.yaml files under ai-services/assets/.",
            ])
        
        error_msg.append("")
        error_msg.append("   Run: python3 .github/scripts/check_image_names.py  (after bumping)")
        
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
    
    print(f"Changed files: {len(changed_files)}")
    print()
    
    errors = []
    passed_components = []
    failed_components = []
    
    # Check each component
    for component_path, values_yaml_key, name in COMPONENTS:
        needs_check, error_msg = check_component_version_bump(
            component_path, values_yaml_key, name, changed_files, base_ref, repo_root
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
    print(f"✅ All {len(passed_components)} component(s) have proper version bumps!")
    print("=" * 70)
    return 0


if __name__ == "__main__":
    sys.exit(main())

# Made with Bob