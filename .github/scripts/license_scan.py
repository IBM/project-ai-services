import argparse
import json
import sys


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


def merge_sbom_data(trivy_data, parlay_data):
    merged_data = {}
    for key, value in trivy_data.items():
        merged_data[key] = value
        merged_data[key].setdefault("License by Parlay", "-")
    for key, value in parlay_data.items():
        if key in merged_data:
            merged_data[key].update(value)
        else:
            merged_data[key] = value
            merged_data[key].setdefault("License by Trivy", "-")
    return merged_data


def classify_license(licenses_data):
    deny_license_list = load_licenses_file("deny.txt")
    warn_license_list = load_licenses_file("warn.txt")
    classified_pkg = {
        "deny": {},
        "warn": {},
        "unknown": {},
        "allowed": {}
    }

    for key, value in licenses_data.items():
        pkg_license = value["License by Trivy"] + \
            " " + value["License by Parlay"]
        if is_licence_exist(pkg_license, deny_license_list):
            classified_pkg["deny"][key] = value
        elif is_licence_exist(pkg_license, warn_license_list):
            classified_pkg["warn"][key] = value
        elif is_licence_exist(pkg_license, ["UNKNOWN", "Unlicense"]):
            classified_pkg["unknown"][key] = value
        else:
            classified_pkg["allowed"][key] = value
    return classified_pkg


def scan_pkg_license(licenses_data):
    classified_pkg = classify_license(licenses_data)

    for category, pkg_list in classified_pkg.items():
        if category != "allowed":
            print_result(pkg_list, f"\n\n{'*'*40} {category.capitalize()} {'*'*40}\n\n")
    
    print("\n\nSummary of pkg license classification")
    print(f"{'-'*40}")
    print(f"Denied  : {len(classified_pkg['deny'])}")
    print(f"Warn    : {len(classified_pkg['warn'])}")
    print(f"Unknown : {len(classified_pkg['unknown'])}")
    print(f"Allowed : {len(classified_pkg['allowed'])}")
    print(f"Total   : {len(licenses_data)}")

    if len(classified_pkg["deny"]) > 0:
        print("Error: please remove the package which have denied licenses")
        sys.exit(1)


def is_licence_exist(pkg_license, license_list):
    for license in license_list:
        if license in pkg_license:
            return True
    return False


def load_licenses_file(file_name):
    try:
        with open(f".github/scripts/license-list/{file_name}", 'r') as licenses:
            licenses_list = [license.strip() for license in licenses.readlines()]
            return licenses_list
    except Exception as e:
        print(f"Error: {e}")
        sys.exit(1)

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


if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        prog='SBOM Gen', description='Generate readable SBOM combining Trivy and Parlay data')
    parser.add_argument(
        'trivy_file', help="File path for Trivy CycloneDX input", default="sbom-trivy.json")
    parser.add_argument(
        'parlay_file', help="File path for Parlay CycloneDX input")
    args = parser.parse_args()

    with open(args.trivy_file, encoding="utf-8") as trivy_file:
        trivy_data = parse_cyclonedx(json.load(trivy_file), "Trivy")

    with open(args.parlay_file, encoding="utf-8") as parlay_file:
        parlay_data = parse_cyclonedx(json.load(parlay_file), "Parlay")

    merged_data = merge_sbom_data(trivy_data, parlay_data)

    scan_pkg_license(merged_data)
