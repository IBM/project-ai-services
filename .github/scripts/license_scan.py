import argparse
import json
import sys

# Parse CycloneDX SBOM and extract package licenses
def parse_cyclonedx(data, tool_name):
    sbom = {}
    if "components" in data:
        for component in data["components"]:
            try:
                licenses = ', '.join(
                    map(
                        lambda lic: lic["expression"] if "expression" in lic else (
                            lic["license"].get("id") if isinstance(
                                lic.get("license"), dict) and "id" in lic["license"] else lic["license"]["name"]
                        ),
                        component.get("licenses", [])
                    )
                )
            except KeyError:
                licenses = "-"
            name = component.get("name", "-")
            version = component.get("version", "-")
            if name != "-" and version != "-":
                sbom[f"{name}@{version}"] = {f"License by {tool_name}": licenses}
    return sbom


# Scan and classify package licenses to allowed, warn, deny, unknown
def scan_pkg_license(trivy_data, parlay_data):
    classified_pkg = classify_license(trivy_data, parlay_data)

    # Print classified package licenses
    for category, pkg_list in classified_pkg.items():
        print_result(pkg_list, f"\n\n{'*'*40} {category.capitalize()} {'*'*40}\n\n")
    
    # Print summary of classification
    print("\n\nSummary of pkg license classification")
    print(f"{'-'*40}")
    print(f"Denied  : {len(classified_pkg['deny'])}")
    print(f"Warn    : {len(classified_pkg['warn'])}")
    print(f"Unknown : {len(classified_pkg['unknown'])}")
    print(f"Allowed : {len(classified_pkg['allowed'])}")
    print(f"Total   : {len(trivy_data)}")

    # Exit with error if any denied licenses found
    if len(classified_pkg["deny"]) > 0:
        print("Error: please remove the package which have denied licenses")
        sys.exit(1)

# Classify package licenses into deny, warn, unknown, allowed
def classify_license(trivy_data, parlay_data):
    deny_license_list = load_licenses_file("deny.txt")
    warn_license_list = load_licenses_file("warn.txt")
    # Loading packages with approved licenses
    approved_pkgs = load_approved_pkgs("approved_pkg.json")

    classified_pkg = {
        "deny": {},
        "warn": {},
        "unknown": {},
        "allowed": {}
    }

    for pkg_name, value in trivy_data.items():
        pkg_licenses = value + " " + parlay_data.get(pkg_name, "")
        pkg_license_dic = {
            "License by Trivy": value,
            "License by Parlay": parlay_data.get(pkg_name, ""),
        }
        
        if is_pkg_license_approved(pkg_name, pkg_licenses, approved_pkgs):
            classified_pkg["allowed"][pkg_name] = pkg_license_dic
        elif is_licence_exist(deny_license_list, pkg_licenses):
            classified_pkg["deny"][pkg_name] = pkg_license_dic
        elif is_licence_exist(warn_license_list, pkg_licenses):
            classified_pkg["warn"][pkg_name] = pkg_license_dic
        elif is_licence_exist(["UNKNOWN", "Unlicense"], pkg_licenses):
            classified_pkg["unknown"][pkg_name] = pkg_license_dic
        else:
            classified_pkg["allowed"][pkg_name] = pkg_license_dic
    return classified_pkg


# Load license list from the given file
def load_licenses_file(file_name):
    try:
        with open(f".github/scripts/license-list/{file_name}", 'r') as licenses:
            licenses_list = [license.strip() for license in licenses.readlines()]
            return licenses_list
    except Exception as e:
        print(f"Error: {e}")
        sys.exit(1)


# Loads approved packages from the JSON file
def load_approved_pkgs(file_name):
    try:
        with open(f".github/scripts/license-list/{file_name}", 'r') as approved_pkg_file:
            approved_pkg_data = json.load(approved_pkg_file)
            return approved_pkg_data.get("approved_pkg", {})
    except Exception as e:
        print(f"Error: {e}")
        sys.exit(1)


def is_licence_exist(license_list, pkg_license):
    for license in license_list:
        if license in pkg_license:
            return True
    return False


def print_result(licence_list, msg):
    if len(licence_list) == 0:
        return
    print(msg)
    print("{:<50} | {:<30} | {:<30} | {:<30}".format(
        "Name", "Version", "License by Trivy", "License by Parlay"))
    print(f"{'-'*50} + {'-'*30} + {'-'*30} + {'-'*30}")
    for key, value in licence_list.items():
        name, version = key.split("@")
        print("{:<50} | {:<30} | {:<30} | {:<30}".format(name, version, value.get(
            "License by Trivy", "-"), value.get("License by Parlay", "-")))


def is_pkg_license_approved(pkg_name, pkg_licenses, approved_pkg):
    if pkg_name not in approved_pkg:
        return False
    licenses = approved_pkg[pkg_name]
    if isinstance(licenses, list):
        for lic in licenses:
            if lic in pkg_licenses:
                return True
    elif licenses in pkg_licenses:
        return True
    return False


if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        prog='SBOM Gen', description='Generate readable SBOM combining Trivy and Parlay data')
    parser.add_argument(
        'trivy_file', nargs='?', help="File path for Trivy CycloneDX input", default="trivy.json")
    parser.add_argument(
        'parlay_file',nargs='?', help="File path for Parlay CycloneDX input", default="parlay.json")
    args = parser.parse_args()

    with open(args.trivy_file, encoding="utf-8") as trivy_file:
        trivy_data = parse_cyclonedx(json.load(trivy_file), "Trivy")

    with open(args.parlay_file, encoding="utf-8") as parlay_file:
        parlay_data = parse_cyclonedx(json.load(parlay_file), "Parlay")

    scan_pkg_license(trivy_data, parlay_data)
