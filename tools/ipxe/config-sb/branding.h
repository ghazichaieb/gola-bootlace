/* Added to the build for the Secure Boot route (copied to src/config/local/ by build-sb.sh).
 *
 * A signed iPXE must carry its own security generation (SBAT, the revocation mechanism of shim and the firmware): when a hole is found in a build we signed, raising
 * this number and having PCs refuse the older ones is possible without waiting for upstream. Raise PRODUCT_SBAT_GENERATION whenever a signed build is found unsafe. */
#undef PRODUCT_SBAT_NAME
#define PRODUCT_SBAT_NAME "gola"
#undef PRODUCT_SBAT_GENERATION
#define PRODUCT_SBAT_GENERATION 1
