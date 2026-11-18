#!/usr/bin/env python3
"""
Validates that image names and versions in Makefiles are consistent with
the image references in ai-services/assets application template values.yaml files.

When a Makefile TAG is updated, the corresponding image references in all
values.yaml files must also be updated to match.
"""

import re
import subprocess
import sys
from pathlib import Path
from typing import Dict, List, Optional, Tuple

# Registry used in values.yaml files
EXPECTED_REGISTRY = "icr.io/ai-services-cicd"

# Directories to ignore during Makefile discovery
IGNORED_DIRS = {"vendor", "venv", ".venv", "node_modules", ".git"}

# Makefiles to ignore (aggregators / dispatchers / tests without image definitions)
IGNORED_MAKEFILES = {
    Path("services/Makefile"),
    Path("images/Makefile"),
    Path("ai-services/tests/e2e/cli/Makefile"),
}


def find_makefiles(repo_root: Path) -> List[Path]:
    """Find all relevant Makefiles across the repository."""
    makefiles = []
    for makefile_path in sorted(repo_root.rglob("Makefile")):
        rel_path = makefile_path.relative_to(repo_root)
        if any(part in IGNORED_DIRS for part in rel_path.parts):
            continue
        if rel_path in IGNORED_MAKEFILES:
            continue
        makefiles.append(rel_path)
    return makefiles


def extract_image_name(makefile_path: Path) -> Optional[str]:
    """Extract IMAGE= variable from a Makefile."""
    content = makefile_path.read_text()
    match = re.search(r"^\s*IMAGE\s*\??=\s*(\S+)", content, re.MULTILINE)
    if match:
        return match.group(1).strip()
    return None


def resolve_tag_fallback(makefile_path: Path) -> Optional[str]:
    """Parse TAG= and resolve variable references statically if make command fails."""
    content = makefile_path.read_text()
    variables = {}
    for line in content.splitlines():
        var_match = re.match(r"^(\w+)\s*\??\s*=\s*(.+?)(?:\s*#.*)?$", line.strip())
        if var_match:
            variables[var_match.group(1)] = var_match.group(2).strip()

    tag_value = variables.get("TAG")
    if not tag_value:
        return None

    pattern = r"\$\((\w+)\)"
    while re.search(pattern, tag_value):
        match = re.search(pattern, tag_value)
        if match:
            var_name = match.group(1)
            replacement = variables.get(var_name, match.group(0))
            tag_value = tag_value.replace(match.group(0), replacement)
    return tag_value


def get_makefile_tag(repo_root: Path, makefile_rel: Path) -> Optional[str]:
    """Execute 'make image-tag' to retrieve the evaluated tag value."""
    full_path = repo_root / makefile_rel

    # Handle services/<service_name>/Makefile via parent services/Makefile
    if makefile_rel.parts[0] == "services" and len(makefile_rel.parts) == 3:
        service_name = makefile_rel.parts[1]
        services_dir = repo_root / "services"
        res = subprocess.run(
            ["make", "-s", "-C", str(services_dir), "image-tag", f"SERVICE={service_name}"],
            capture_output=True,
            text=True,
        )
        if res.returncode == 0 and res.stdout.strip():
            return res.stdout.strip()

    # Standalone Makefiles with image-tag target
    res = subprocess.run(
        ["make", "-s", "-C", str(full_path.parent), "image-tag"],
        capture_output=True,
        text=True,
    )
    if res.returncode == 0 and res.stdout.strip():
        return res.stdout.strip()

    # Fallback to static resolution
    return resolve_tag_fallback(full_path)


def discover_image_catalog(repo_root: Path) -> Dict[str, Tuple[str, str]]:
    """
    Discover all images and their tags from repository Makefiles.

    Returns:
        Dict mapping image_name -> (relative_makefile_path, resolved_tag)
    """
    catalog = {}
    for makefile_rel in find_makefiles(repo_root):
        full_path = repo_root / makefile_rel
        image_name = extract_image_name(full_path)
        if not image_name:
            continue

        tag = get_makefile_tag(repo_root, makefile_rel)
        if not tag:
            continue

        catalog[image_name] = (str(makefile_rel), tag)
    return catalog


def scan_assets_for_images(
    repo_root: Path,
) -> List[Tuple[str, int, str, str, str]]:
    """
    Scan all YAML files under ai-services/assets for ICR image references.

    Returns:
        List of tuples: (relative_file_path, line_number, full_image_ref, image_name, tag)
    """
    assets_dir = repo_root / "ai-services" / "assets"
    if not assets_dir.exists():
        return []

    # Match: icr.io/<registry_path>/<image_name>:<tag>
    pattern = re.compile(r"(icr\.io/[^\s\"'\)]+/([^:\s\"'\)]+):([^\s\"'\)]+))")
    found_images = []

    yaml_files = sorted(
        list(assets_dir.rglob("*.yaml")) + list(assets_dir.rglob("*.yml"))
    )

    for yaml_path in yaml_files:
        rel_path = str(yaml_path.relative_to(repo_root))
        try:
            lines = yaml_path.read_text().splitlines()
        except Exception:
            continue

        for line_no, line in enumerate(lines, 1):
            for match in pattern.finditer(line):
                full_ref = match.group(1)
                image_name = match.group(2)
                tag = match.group(3)
                found_images.append((rel_path, line_no, full_ref, image_name, tag))

    return found_images


def main() -> int:
    repo_root = Path(__file__).resolve().parent.parent.parent
    errors = []

    print("=" * 70)
    print("Checking image name and version consistency across templates...")
    print("=" * 70)
    print()

    # 1. Discover all Makefiles and build image catalog
    image_catalog = discover_image_catalog(repo_root)

    # 2. Scan assets directory for image references
    asset_references = scan_assets_for_images(repo_root)

    # Group references by image name
    refs_by_image: Dict[str, List[Tuple[str, int, str, str]]] = {}
    for rel_file, line_no, full_ref, img_name, tag in asset_references:
        refs_by_image.setdefault(img_name, []).append((rel_file, line_no, full_ref, tag))

    # 3. Validate each Makefile component against asset templates
    for image_name, (makefile_rel, expected_tag) in sorted(image_catalog.items(), key=lambda x: x[1][0]):
        print(f"📦 {makefile_rel}")
        print(f"   IMAGE={image_name}  TAG={expected_tag}")
        print()

        refs = refs_by_image.get(image_name, [])
        if not refs:
            print("   ⏭  No template references found in ai-services/assets/")
            print()
            continue

        for rel_file, line_no, full_ref, found_tag in refs:
            if found_tag != expected_tag:
                errors.append(
                    f"   ❌ Version mismatch in {rel_file} [line {line_no}]:\n"
                    f"      Makefile TAG   : {expected_tag}\n"
                    f"      values.yaml    : {found_tag}\n"
                    f"      Fix: update image reference to "
                    f"'{EXPECTED_REGISTRY}/{image_name}:{expected_tag}'"
                )
                print(
                    f"   ❌ {rel_file}:{line_no}: "
                    f"tag '{found_tag}' != '{expected_tag}'"
                )
            else:
                print(
                    f"   ✅ {rel_file}:{line_no}: "
                    f"{full_ref}"
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
