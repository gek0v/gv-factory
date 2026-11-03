<!--
SPDX-License-Identifier: Apache-2.0
SPDX-FileCopyrightText: 2026 gv-factory contributors
-->

# ADR 0009: Empaquetado Multi-Arquitectura con GoReleaser, Firma Criptográfica sin Claves con Cosign y Atestación SLSA Nivel 3

* **Estado:** Aceptado
* **Fecha:** 2026-11-01
* **Decisores:** Equipo de Arquitectura de gv-factory

*(Nota de correlación: Registrado formalmente como ADR-0009 según especificación de distribución y automatización de versiones; referencia complementaria indexada como ADR-0010).*

## Contexto
La plataforma modular de fábricas de software autónomas `gv-factory` evoluciona hacia su versión de producción 1.0, requiriendo un proceso de distribución y empaquetado continuo que satisfaga los estándares modernos de seguridad en la cadena de suministro de software (OpenSSF, SLSA Level 3).

Los requerimientos fundamentales abarcan:
1. **Compilación cruzada reproducible y empaquetado multi-plataforma:** Los tres componentes binarios (`ov-server`, `ov-worker` y `ov-cli`) deben compilarse de manera reproducible sin dependencias de C (`CGO_ENABLED=0`) para arquitecturas Linux y macOS (`amd64`, `arm64`), así como Windows (`amd64`) para la herramienta de consola `ov-cli`.
2. **Inyección homogénea de metadatos de compilación:** Es imprescindible vincular de forma inmutable la versión semántica, el hash del commit Git, la marca temporal de compilación y el agente constructor tanto en el paquete compartido `pkg/version` como en las variables de nivel de paquete de los ejecutables.
3. **Firmado criptográfico sin gestión de claves estáticas (Keyless Signing):** La custodia de claves privadas de larga duración en secretos de CI/CD introduce vulnerabilidades críticas de exfiltración. Se requiere un modelo de firma digital sin claves estáticas basado en identidades OIDC efímeras y el registro transparente de auditoría Sigstore (Rekor / Fulcio).
4. **Catálogo de materiales de software (SBOM) y atestaciones SLSA Level 3:** Generación de inventarios estandarizados SPDX JSON con Syft para cada archivo distribuido y para las imágenes de contenedor OCI, asegurando atestaciones criptográficas de procedencia firmadas y verificables por operadores.
5. **Distribución automatizada y ergonómica:** Soporte para Homebrew Tap Casks (`gek0v/homebrew-tap`), cálculo estandarizado de `checksums.txt` SHA-256 e integración con la publicación semántica gobernada por Release Please.

## Decisión

### 1. Especificación Declarativa GoReleaser v2 (`.goreleaser.yaml`)
Adoptar la especificación moderna v2 de GoReleaser:
- Configuración de compilaciones independientes con identificadores explícitos: `ov-server`, `ov-worker` y `ov-cli`.
- Inyección unificada de flags de enlace (`ldflags`):
  `-s -w -X main.version={{.Version}} -X main.commit={{.Commit}} -X main.date={{.Date}} -X main.builtBy=goreleaser -X github.com/gv-factory/gv-factory/pkg/version.Version={{.Version}} ...`
- Empaquetado granular en `.tar.gz` para Unix y `.zip` para entornos Windows, incluyendo automáticamente los ficheros de licencia y documentación raíz.
- Generación declarativa de SBOM en formato SPDX JSON vinculados a cada artefacto empaquetado.
- Firma automática del manifiesto `checksums.txt` mediante `cosign sign-blob` con emisión de certificados X.509 efímeros (`.pem`) y firmas criptográficas (`.sig`).
- Definición de Cask para Homebrew en `homebrew_casks` orientada al repositorio `gek0v/homebrew-tap`.

### 2. Firma Criptográfica Keyless con Sigstore Cosign
- En el entorno de GitHub Actions, los flujos obtienen un token OIDC efímero con permisos `id-token: write`.
- Fulcio actúa como Autoridad de Certificación (CA) de corta duración asociando la firma al flujo de trabajo del repositorio (`https://github.com/gv-factory/gv-factory/.github/workflows/release.yml@refs/tags/...`).
- Rekor registra la entrada en el libro mayor de transparencia (Transparency Log), garantizando no-repudio sin requerir la gestión ni rotación de claves privadas privadas simétricas o asimétricas.

### 3. Pipeline de Release Automatizado (`.github/workflows/release.yml`)
Implementar una tubería de dos etapas concurrentes activada por etiquetas `v*.*.*` o despacho semántico desde `release-please.yml`:
- **Job `goreleaser`:** Ejecución de GoReleaser v2, descarga automatizada de `cosign` y `syft`, generación de binarios, archivos comprimidos, SBOMs por artefacto y firma del archivo de sumas de verificación.
- **Job `publish-and-sign-containers`:**
  - Construcción multi-arquitectura (`linux/amd64`, `linux/arm64`) mediante QEMU y Docker Buildx para las imágenes de producción `ov-server`, `ov-worker` y `ov-web`.
  - Firma del digest inmutable de la imagen con `cosign sign --yes`.
  - Generación del SBOM SPDX JSON de la imagen con Syft y atestación en el registro OCI mediante `cosign attest --yes --predicate <sbom> --type https://spdx.dev/Document`.
  - Generación de atestación de procedencia SLSA Nivel 3 mediante la acción oficial `actions/attest-build-provenance@v2`.
  - Publicación del archivo SBOM como adjunto directo a la Release de GitHub.

### 4. Guía de Verificación Criptográfica para Operadores

Los operadores y consumidores de los artefactos pueden verificar la autenticidad e integridad mediante los siguientes comandos:

#### A. Verificación del Checksum y Binarios
```bash
# 1. Descargar el archivo, el checksum, el certificado y la firma
curl -LO https://github.com/gv-factory/gv-factory/releases/download/v1.0.0/checksums.txt
curl -LO https://github.com/gv-factory/gv-factory/releases/download/v1.0.0/checksums.txt.pem
curl -LO https://github.com/gv-factory/gv-factory/releases/download/v1.0.0/checksums.txt.sig

# 2. Verificar la firma del checksum con Cosign en modo keyless
cosign verify-blob \
  --certificate checksums.txt.pem \
  --signature checksums.txt.sig \
  --certificate-identity-regexp '^https://github.com/gv-factory/gv-factory/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt

# 3. Validar la integridad del binario descargado
sha256sum --check checksums.txt --ignore-missing
```

#### B. Verificación de Imágenes de Contenedor OCI
```bash
# Verificar la firma del contenedor en GitHub Container Registry
cosign verify \
  --certificate-identity-regexp '^https://github.com/gv-factory/gv-factory/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ghcr.io/gv-factory/gv-factory/ov-server:v1.0.0

# Verificar la atestación del SBOM SPDX
cosign verify-attestation \
  --type https://spdx.dev/Document \
  --certificate-identity-regexp '^https://github.com/gv-factory/gv-factory/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ghcr.io/gv-factory/gv-factory/ov-server:v1.0.0
```

#### C. Verificación de la Procedencia SLSA Nivel 3
```bash
gh attestation verify oci://ghcr.io/gv-factory/gv-factory/ov-server:v1.0.0 \
  --owner gv-factory
```

## Consecuencias

* **Positivas:**
  - Cumplimiento estricto de las directrices SLSA Nivel 3 y OpenSSF para software de misión crítica.
  - Eliminación completa de secretos estáticos y riesgo de fuga de claves privadas gracias a Sigstore Keyless Signing.
  - Trazabilidad y transparencia absoluta de dependencias mediante catálogos SBOM SPDX JSON generados tanto para binarios como para contenedores.
  - Distribución multiplataforma transparente para Linux, macOS y Windows con metadatos de versión inyectados en tiempo de enlace.
  - Automatización desacoplada y reutilizable invocable tanto por etiquetas Git manuales como por el flujo automatizado de Release Please.
* **Negativas / Costes Operativos:**
  - La compilación cruzada multi-arquitectura con emulación QEMU en GitHub Actions incrementa el tiempo de ejecución del workflow de release (mitigado con paralelismo por matriz y cache de capas).
  - Los operadores que verifiquen imágenes o binarios en entornos desconectados (air-gapped) requieren sincronizar previamente los certificados y registros de transparencia de Rekor.
