"""Static guard: the verifier must never disable TLS verification or trust `sub` for access."""

import pathlib
import re

AUTH_DIR = pathlib.Path(__file__).resolve().parents[2] / "auth"
FORBIDDEN = [
    r"verify\s*=\s*False",
    r"InsecureSkipVerify",
    r"CERT_NONE",
    r"check_hostname\s*=\s*False",
    r"_create_unverified_context",
    r"verify_signature['\"]?\s*[:=]\s*False",
    r"options\s*=\s*\{[^}]*verify_(aud|exp|iss|nbf)",
    r"\.subject\s*==",
    r"\[['\"]sub['\"]\]\s*==",
]


def test_auth_package_has_no_insecure_patterns():
    files = list(AUTH_DIR.glob("*.py"))
    assert files
    for f in files:
        text = f.read_text()
        for pat in FORBIDDEN:
            assert not re.search(pat, text), f"{f.name} matches forbidden pattern {pat!r}"
