# winget

The manifests for Pelton's first version in the [Windows Package Manager community repository](https://github.com/microsoft/winget-pkgs), package `peltonapp.Pelton`.

Later versions are sent by `.github/workflows/winget.yml` when a release is published. It copies the version already in winget, with the new installer's URL and hash. These files only matter again if the package has to be submitted from scratch.

To submit them:
1. Set the version, the release date, the installer URL and its SHA-256 for the release being submitted.
2. Check them with `winget validate --manifest packaging/winget` on Windows.
3. Open a pull request at microsoft/winget-pkgs adding them under `manifests/p/peltonapp/Pelton/<version>/`.

The installer takes `/S` for a silent install, plus `/CurrentUser` or `/AllUsers` for the scope. A per-user install needs no admin rights. An all-users install elevates itself.
