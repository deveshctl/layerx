# Verifying releases

Every release ships a cosign-signed `checksums.txt`, an SPDX SBOM per archive, and a SLSA Build Level 3 provenance attestation. Verifying both proves the archive came out of this repository's release workflow on GitHub-hosted runners.

```bash
TAG=v1.6.3   # the release you downloaded
ARCHIVE=layerx_linux_amd64.tar.gz
BASE="https://github.com/deveshctl/layerx/releases/download/${TAG}"

curl -sLO "${BASE}/${ARCHIVE}"
curl -sLO "${BASE}/checksums.txt"
curl -sLO "${BASE}/checksums.txt.sigstore.json"
curl -sLO "${BASE}/multiple.intoto.jsonl"   # SLSA provenance

# 1) Verify the cosign keyless signature over checksums.txt.
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp "^https://github.com/deveshctl/layerx/" \
  --certificate-oidc-issuer     "https://token.actions.githubusercontent.com" \
  checksums.txt

# 2) Verify the archive matches its checksum.
grep "  ${ARCHIVE}$" checksums.txt | sha256sum --check

# 3) (Optional) Verify SLSA provenance with slsa-verifier.
slsa-verifier verify-artifact "${ARCHIVE}" \
  --provenance-path multiple.intoto.jsonl \
  --source-uri github.com/deveshctl/layerx \
  --source-tag "${TAG}"
```

The signature chain is anchored in [Sigstore](https://www.sigstore.dev/)'s public transparency log; no long-lived keys are involved.

[Installation](install.md) · [Security policy](../SECURITY.md)
