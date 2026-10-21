# Test corpus

The archives hold third-party Lua code used to compare the parser with
LuaJIT. Each archive keeps the license of its source. They are test data
and are not part of the library.

| Archive | Source | Revision | License |
|:---|:---|:---|:---|
| `lua5.1-tests.tar.gz` | <https://www.lua.org/tests/lua5.1-tests.tar.gz>, unchanged | Lua 5.1 | MIT (Lua license) |
| `luajit-tests.tar.gz` | `test/LuaJIT-tests`, `test/lua-Harness-tests` and `test/tarantool-tests` of <https://github.com/tarantool/luajit> | `8af934d93cf26fb1e9b08b28a8b51833d55fc8e8` | MIT, `COPYRIGHT` in the archive |
| `tarantool.tar.gz` | `src/lua` and `src/box/lua` of <https://github.com/tarantool/tarantool> | `36f5233ac875e4d5b1a9310697a84ccdb77ef5b5` | BSD-2-Clause, `LICENSE` in the archive |
| `vshard.tar.gz` | all `*.lua` files of <https://github.com/tarantool/vshard> | `4732c58cdaafbb8fd1932028f6471a6c44248d9a` | BSD-2-Clause, `LICENSE` in the archive |
| `crud.tar.gz` | all `*.lua` files of <https://github.com/tarantool/crud> | `e7b4b8b57e0d73785f8891832a3de1c5953ac489` | BSD-2-Clause, `LICENSE` in the archive |

The archives are reproducible:

```sh
tar --sort=name --owner=0 --group=0 --numeric-owner \
    --mtime='2026-01-01 00:00Z' -cf - <files> | gzip -9n > <archive>
```
