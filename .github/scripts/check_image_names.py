#!/usr/bin/env python3
"""
Validates that image names and versions in Makefiles are consistent with
the image references in ai-services/assets application template values.yaml files.

When a Makefile TAG is updated, the corresponding image references in all
values.yaml files must also be updated to match.
"""

import re
import sys
from pathlib import Path
from typing import Optional, Tuple

# Registry used in values.yaml files
EXPECTED_REGISTRY = "icr.io/ai-services-cicd"

# Map of: makefile_path -> list of (values_yaml_path, values_key)
# values_key is the top-level key in values.yaml that contains the image reference
COMPONENTS = {
    "services/chatbot/Makefile": [
        ("ai-services/assets/applications/rag/podman/values.yaml", "backend"),
        ("ai-services/assets/applications/rag/openshift/values.yaml", "backend"),
        ("ai-services/assets/applications/rag-dev/podman/values.yaml", "backend"),
        ("ai-services/assets/applications/rag-dev/openshift/values.yaml", "backend"),
        ("ai-services/assets/applications/rag-cpu/podman/values.yaml", "backend"),
        ("ai-services/assets/services/chat/podman/values.yaml", "backend"),
    ],
    "services/digitize/Makefile": [
        ("ai-services/assets/applications/rag/podman/values.yaml", "digitize"),
        ("ai-services/assets/applications/rag/openshift/values.yaml", "digitize"),
        ("ai-services/assets/applications/rag-dev/podman/values.yaml", "digitize"),
        ("ai-services/assets/applications/rag-dev/openshift/values.yaml", "digitize"),
        ("ai-services/assets/applications/rag-cpu/podman/values.yaml", "digitize"),
        ("ai-services/assets/services/digitize/podman/values.yaml", "digitize"),
    ],
    "services/summarize/Makefile": [
        ("ai-services/assets/applications/rag/podman/values.yaml", "summarize"),
        ("ai-services/assets/applications/rag/openshift/values.yaml", "summarize"),
        ("ai-services/assets/applications/rag-dev/podman/values.yaml", "summarize"),
        ("ai-services/assets/applications/rag-dev/openshift/values.yaml", "summarize"),
        ("ai-services/assets/applications/rag-cpu/podman/values.yaml", "summarize"),
        ("ai-services/assets/services/summarize/podman/values.yaml", "summarize"),
    ],
    "services/similarity/Makefile": [
        ("ai-services/assets/applications/rag/podman/values.yaml", "similarity"),
        ("ai-services/assets/applications/rag/openshift/values.yaml", "similarity"),
        ("ai-services/assets/applications/rag-dev/podman/values.yaml", "similarity"),
        ("ai-services/assets/applications/rag-dev/openshift/values.yaml", "similarity"),
        ("ai-services/assets/applications/rag-cpu/podman/values.yaml", "similarity"),
        ("ai-services/assets/services/similarity/podman/values.yaml", "similarity"),
    ],
    "ui/chatbot/Makefile": [
        ("ai-services/assets/applications/rag/podman/values.yaml", "ui"),
        ("ai-services/assets/applications/rag-dev/podman/values.yaml", "ui"),
        ("ai-services/assets/applications/rag/openshift/values.yaml", "ui"),
        ("ai-services/assets/applications/rag-dev/openshift/values.yaml", "ui"),
        ("ai-services/assets/applications/rag-cpu/podman/values.yaml", "ui"),
    ],
    "ui/digitize/Makefile": [
        ("ai-services/assets/applications/rag/openshift/values.yaml", "digitizeUi"),
        ("ai-services/assets/applications/rag/podman/values.yaml", "digitizeUi"),
        ("ai-services/assets/applications/rag-dev/openshift/values.yaml", "digitizeUi"),
        ("ai-services/assets/applications/rag-dev/podman/values.yaml", "digitizeUi"),
        ("ai-services/assets/applications/rag-cpu/podman/values.yaml", "digitizeUi"),
    ],
    "ui/catalog/Makefile": [
        ("ai-services/assets/catalog/podman/values.yaml", "ui"),
    ],
    "ai-services/Makefile": [
        ("ai-services/assets/catalog/podman/values.yaml", "backend"),
    ],
    "images/postgres/Makefile": [
        ("ai-services/assets/catalog/podman/values.yaml", "db"),
        ("ai-services/assets/applications/rag/podman/values.yaml", "postgres"),
        ("ai-services/assets/applications/rag/openshift/values.yaml", "postgres"),
        ("ai-services/assets/applications/rag-dev/podman/values.yaml", "postgres"),
        ("ai-services/assets/applications/rag-dev/openshift/values.yaml", "postgres"),
        ("ai-services/assets/applications/rag-cpu/podman/values.yaml", "postgres"),
    ],
    "images/litellm/Makefile": [
        ("ai-services/assets/applications/rag-cpu/podman/values.yaml", "litellm"),
    ],
    "images/caddy/Makefile": [
        ("ai-services/assets/catalog/podman/values.yaml", "caddy"),
    ]
}


def get_makefile_info(makefile_path: Path) -> Tuple[str, str]:
    """Extract IMAGE= and TAG= values from a Makefile, calculating TAG if it references other variables."""
    content = makefile_path.read_text()

    # Extract all variable definitions
    variables = {}
    for line in content.split('\n'):
        # Match variable assignments: VAR?=value or VAR=value
        var_match = re.match(r'^(\w+)\s*\??\s*=\s*(.+?)(?:\s*#.*)?$', line.strip())
        if var_match:
            var_name = var_match.group(1)
            var_value = var_match.group(2).strip()
            variables[var_name] = var_value

    # Get IMAGE value
    image = variables.get('IMAGE')
    if not image:
        raise ValueError(f"Could not find IMAGE= in {makefile_path}")

    # Get TAG value
    tag_value = variables.get('TAG')
    if not tag_value:
        raise ValueError(f"Could not find TAG= in {makefile_path}")

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
    return image, resolved_tag


def get_image_from_values_yaml(values_path: Path, key: str) -> Tuple[Optional[str], Optional[str]]:
    """
    Extract image name and tag from a values.yaml section.

    Example: key=backend, image line: icr.io/ai-services-cicd/rag:v0.0.32
    Returns: ("rag", "v0.0.32")
    """
    content = values_path.read_text()

    # Find the section for the key and extract the image line within it
    pattern = re.compile(
        rf"^{key}:\s*\n(.*?)(?=^\w|\Z)",
        re.MULTILINE | re.DOTALL,
    )
    section_match = pattern.search(content)
    if not section_match:
        raise ValueError(f"Could not find '{key}:' section in {values_path}")

    section = section_match.group(1)
    image_match = re.search(r"image:\s*(\S+)", section)
    if not image_match:
        raise ValueError(f"Could not find 'image:' in '{key}' section of {values_path}")

    full_image = image_match.group(1)

    # Only validate images from our own registry
    if not full_image.startswith(EXPECTED_REGISTRY + "/"):
        return None, None  # Skip third-party images

    # Parse: icr.io/ai-services-cicd/IMAGE:TAG
    image_with_tag = full_image.split("/")[-1]
    if ":" not in image_with_tag:
        raise ValueError(f"Image missing tag in {values_path}: {full_image}")

    image_name, tag = image_with_tag.split(":", 1)
    return image_name, tag


def main() -> int:
    repo_root = Path(__file__).parent.parent.parent
    errors = []

    print("=" * 70)
    print("Checking image name and version consistency across templates...")
    print("=" * 70)
    print()

    for makefile_rel, values_entries in COMPONENTS.items():
        makefile_path = repo_root / makefile_rel

        if not makefile_path.exists():
            errors.append(f"❌ Makefile not found: {makefile_rel}")
            continue

        try:
            makefile_image, makefile_tag = get_makefile_info(makefile_path)
        except ValueError as e:
            errors.append(f"❌ {e}")
            continue

        print(f"📦 {makefile_rel}")
        print(f"   IMAGE={makefile_image}  TAG={makefile_tag}")
        print()

        for values_rel, values_key in values_entries:
            values_path = repo_root / values_rel

            if not values_path.exists():
                errors.append(f"   ❌ File not found: {values_rel}")
                print(f"   ❌ {values_rel}: not found")
                continue

            try:
                values_image, values_tag = get_image_from_values_yaml(values_path, values_key)
            except ValueError as e:
                errors.append(f"   ❌ {values_rel} [{values_key}]: {e}")
                print(f"   ❌ {values_rel} [{values_key}]: parse error - {e}")
                continue

            if values_image is None:
                # Third-party image, skip
                print(f"   ⏭  {values_rel} [{values_key}]: skipped (third-party image)")
                continue

            if values_image != makefile_image:
                errors.append(
                    f"   ❌ Image name mismatch in {values_rel} [{values_key}]:\n"
                    f"      Makefile IMAGE : {makefile_image}\n"
                    f"      values.yaml    : {values_image}"
                )
                print(f"   ❌ {values_rel} [{values_key}]: image '{values_image}' != '{makefile_image}'")
            elif values_tag != makefile_tag:
                errors.append(
                    f"   ❌ Version mismatch in {values_rel} [{values_key}]:\n"
                    f"      Makefile TAG   : {makefile_tag}\n"
                    f"      values.yaml    : {values_tag}\n"
                    f"      Fix: update '{values_key}.image' to "
                    f"'{EXPECTED_REGISTRY}/{makefile_image}:{makefile_tag}'"
                )
                print(
                    f"   ❌ {values_rel} [{values_key}]: "
                    f"tag '{values_tag}' != '{makefile_tag}'"
                )
            else:
                print(
                    f"   ✅ {values_rel} [{values_key}]: "
                    f"{EXPECTED_REGISTRY}/{values_image}:{values_tag}"
                )

        print()

    if errors:
        print("=" * 70)
        print(f"❌ FAILED — {len(errors)} error(s) found:")
        print("=" * 70)
        print()
        for err in errors:
            print(err)
            print()
        print(
            "When updating a Makefile TAG, update the corresponding image\n"
            "references in all values.yaml files under ai-services/assets/."
        )
        return 1

    print("=" * 70)
    print("✅ All image names and versions are consistent!")
    print("=" * 70)
    return 0


if __name__ == "__main__":
    sys.exit(main())

# Made with Bob
