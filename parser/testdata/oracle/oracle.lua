-- Reference results for the parser tests, produced by Tarantool's LuaJIT.
--
-- Usage: luajit oracle.lua <dir with jit/vmdef.lua> <file>...
--
-- For each file prints "== <file>" and then either "error <message>" if
-- loadstring() fails, or "ok" followed by the GGET and GSET instructions
-- of all prototypes: "GGET <line> <name>" and "GSET <line> <name>". These
-- are exactly the reads and writes of global variables in the chunk.
--
-- Control characters, bytes 128-255 and backslashes are printed as \ddd,
-- the same way the Go side escapes them.
package.path = arg[1] .. "/?.lua;" .. package.path

local bit = require("bit")
local jutil = require("jit.util")
local vmdef = require("jit.vmdef")

local function escape(s)
    return (s:gsub("[%c\\\128-\255]", function(c)
        return string.format("\\%03d", c:byte())
    end))
end

local function opname(op)
    return (vmdef.bcnames:sub(op * 6 + 1, op * 6 + 6):gsub("%s+$", ""))
end

local function walk(fn, out)
    for pc = 1, math.huge do
        local ins = jutil.funcbc(fn, pc)
        if not ins then
            break
        end
        local name = opname(bit.band(ins, 0xff))
        if name == "GGET" or name == "GSET" then
            local key = jutil.funck(fn, -bit.rshift(ins, 16) - 1)
            local line = jutil.funcinfo(fn, pc).currentline
            out[#out + 1] = string.format("%s %d %s", name, line, escape(key))
        end
    end
    for i = -1, -math.huge, -1 do
        local k = jutil.funck(fn, i)
        if k == nil then
            break
        end
        if type(k) == "proto" then
            walk(k, out)
        end
    end
end

for i = 2, #arg do
    local f = assert(io.open(arg[i], "rb"))
    local src = f:read("*a")
    f:close()
    io.write("== ", arg[i], "\n")
    local fn, err = loadstring(src, "=m")
    if fn then
        local out = {}
        walk(fn, out)
        io.write("ok\n")
        for _, line in ipairs(out) do
            io.write(line, "\n")
        end
    else
        io.write("error ", escape((err:gsub("^m:", ""))), "\n")
    end
end
