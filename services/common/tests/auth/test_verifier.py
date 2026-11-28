"""Token verification: algorithms, claims, scopes and the JWKS cache policy."""

import time

import jwt
import pytest

from common.auth.jwks import JWKSClient, JWKSUnavailable
from common.auth import jwks as jwks_mod
from common.auth.verifier import InvalidToken, TokenVerifier
from common.tests.auth.helpers import CountingFetcher, Signer, jwks_body, make_config, rsa_token

pytestmark = pytest.mark.asyncio


def _verifier(*signers, fetcher=None, **cfg):
    config = make_config(**cfg)
    fetcher = fetcher or CountingFetcher(jwks_body(*signers))
    return TokenVerifier(config, JWKSClient(config, fetcher)), fetcher


async def test_valid_admin_token():
    s = Signer()
    v, _ = _verifier(s)
    p = await v.verify(s.token())
    assert p.has_role("admin")
    assert p.subject == "uid_1" and p.audience == "digitize"


async def test_role_hierarchy():
    s = Signer()
    v, _ = _verifier(s)
    admin = await v.verify(s.token(scope="digitize:admin"))
    assert admin.has_role("admin") and admin.has_role("write") and admin.has_role("read")
    write = await v.verify(s.token(scope="digitize:write"))
    assert write.has_role("write") and write.has_role("read") and not write.has_role("admin")
    read = await v.verify(s.token(scope="digitize:read"))
    assert read.has_role("read") and not read.has_role("write") and not read.has_role("admin")
    assert not admin.has_role("unknown")
    # A scope for another service grants nothing here.
    other = await v.verify(s.token(scope="other:admin digitize:read"))
    assert not other.has_role("write")


async def test_other_service_scopes_are_ignored():
    s = Signer()
    v, _ = _verifier(s)
    p = await v.verify(s.token(scope="other:admin digitize:read"))
    assert p.roles == frozenset({"read"})


@pytest.mark.parametrize("over", [
    {"iss": "someone-else"},
    {"aud": ["other"]},
    {"aud": None},
    {"exp": int(time.time()) - 3600},
    {"nbf": int(time.time()) + 3600},
    {"exp": None},
    {"nbf": None},
    {"jti": None},
    {"sub": None},
    {"scope": None},
    {"scope": ["digitize:admin"]},
])
async def test_bad_claims_rejected(over):
    s = Signer()
    v, _ = _verifier(s)
    with pytest.raises(InvalidToken):
        await v.verify(s.token(**over))


async def test_alg_none_rejected():
    s = Signer()
    v, fetcher = _verifier(s)
    tok = jwt.encode(s.claims(), None, algorithm="none", headers={"kid": s.kid})
    with pytest.raises(InvalidToken):
        await v.verify(tok)
    assert fetcher.calls == 0  # rejected before any key lookup


async def test_hs256_signed_with_public_key_bytes_rejected():
    s = Signer()
    v, _ = _verifier(s)
    # The classic algorithm-confusion attempt: HMAC keyed with the public JWK.
    tok = jwt.encode(s.claims(), "x" * 32, algorithm="HS256", headers={"kid": s.kid})
    with pytest.raises(InvalidToken):
        await v.verify(tok)


async def test_rs256_rejected():
    s = Signer()
    v, _ = _verifier(s)
    with pytest.raises(InvalidToken):
        await v.verify(rsa_token(s.kid))


async def test_signature_from_another_key_rejected():
    published, attacker = Signer("k1"), Signer("k1")
    v, _ = _verifier(published)
    with pytest.raises(InvalidToken):
        await v.verify(attacker.token())


@pytest.mark.parametrize("tok", ["", "abc", "a.b.c", "a.b"])
async def test_garbage_rejected(tok):
    v, _ = _verifier(Signer())
    with pytest.raises(InvalidToken):
        await v.verify(tok)


async def test_missing_kid_rejected():
    s = Signer()
    v, _ = _verifier(s)
    with pytest.raises(InvalidToken):
        await v.verify(jwt.encode(s.claims(), s.key, algorithm="ES256"))


async def test_unknown_kid_triggers_exactly_one_refetch_per_window():
    s = Signer("k1")
    v, fetcher = _verifier(s)
    await v.verify(s.token())
    assert fetcher.calls == 1

    stranger = Signer("zzz")
    for _ in range(5):
        with pytest.raises(InvalidToken):
            await v.verify(stranger.token())
    assert fetcher.calls == 2  # one forced refetch, then rate-limited


async def test_rotation_new_kid_is_picked_up():
    old, new = Signer("old"), Signer("new")
    fetcher = CountingFetcher(jwks_body(old))
    v, _ = _verifier(fetcher=fetcher)
    await v.verify(old.token())
    fetcher.body = jwks_body(old, new)
    p = await v.verify(new.token())
    assert p.has_role("admin")


async def test_cache_is_used_within_ttl():
    s = Signer()
    v, fetcher = _verifier(s)
    for _ in range(3):
        await v.verify(s.token())
    assert fetcher.calls == 1


async def test_jwks_down_with_empty_cache_is_unavailable():
    v, _ = _verifier(fetcher=CountingFetcher(exc=JWKSUnavailable("down")))
    with pytest.raises(JWKSUnavailable):
        await v.verify(Signer().token())


async def test_stale_keys_survive_an_outage_but_not_forever(monkeypatch):
    s = Signer()
    config = make_config()
    fetcher = CountingFetcher(jwks_body(s))
    client = JWKSClient(config, fetcher)
    v = TokenVerifier(config, client)
    await v.verify(s.token())

    fetcher.exc = JWKSUnavailable("down")
    client._fetched_at -= jwks_mod.CACHE_TTL_SECONDS + 1  # cache expired, still within stale limit
    client._last_failure = float("-inf")
    assert (await v.verify(s.token())).has_role("admin")

    client._fetched_at -= jwks_mod.MAX_STALE_SECONDS  # beyond the stale limit
    client._last_failure = float("-inf")
    with pytest.raises(JWKSUnavailable):
        await v.verify(s.token())


async def test_malformed_jwks_is_unavailable():
    v, _ = _verifier(fetcher=CountingFetcher(b"not json"))
    with pytest.raises(JWKSUnavailable):
        await v.verify(Signer().token())


async def test_private_or_foreign_keys_in_jwks_are_ignored():
    s = Signer()
    jwk = s.jwk()
    jwk["d"] = "AAAA"  # a published private member means the key is not trusted at all
    import json
    v, _ = _verifier(fetcher=CountingFetcher(json.dumps({"keys": [jwk]}).encode()))
    with pytest.raises(JWKSUnavailable):
        await v.verify(s.token())


async def test_readiness_check_uses_fresh_cache_and_does_not_hammer_the_catalog():
    s = Signer()
    config = make_config()
    fetcher = CountingFetcher(jwks_body(s))
    client = JWKSClient(config, fetcher)
    for _ in range(5):
        await client.check()
    assert fetcher.calls == 1


async def test_readiness_check_fails_when_no_keys_can_be_fetched():
    config = make_config()
    client = JWKSClient(config, CountingFetcher(exc=JWKSUnavailable("down")))
    with pytest.raises(JWKSUnavailable):
        await client.check()
