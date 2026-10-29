# JSON dependency

`json.hpp` is the unmodified single-header release of nlohmann/json 3.12.0.
Its MIT license is retained in `LICENSE.json`.

Source: https://github.com/nlohmann/json/tree/v3.12.0

SHA256: `aaf127c04cb31c406e5b04a63f1ae89369fccde6d8fa7cdda1ed4f32dfc5de63`.

## miniz

`miniz/` contains the unmodified tinfl C core, public header and common header
from [miniz 3.1.0](https://github.com/richgel999/miniz/tree/3.1.0), with its MIT
license in `miniz/LICENSE`. Their exact SHA256 values are checked by
`scripts/build-windows-launcher.py`. Local `miniz.h` and `miniz_export.h` adapters
select only static raw-deflate decompression without allocation or ZIP APIs.
They are local configuration, not files represented as upstream releases.
