# Retired Windows PowerShell fixtures

These files preserve the previous implementation and its regression cases. They
are test inputs, not supported launchers or runtime helpers. Root BAT entrypoints
use `scripts/windows-entrypoint.bat` and the native C++ executable. Explicit Git
`build` and `clean` bootstrap the current host inside Docker.

Do not copy this directory into an installation or Release package. Its files
are excluded from the Docker build context and native source fingerprint.
Compatibility tests refer here explicitly and still exercise PowerShell 5.1/7;
passing those tests does not establish current native Windows acceptance.
