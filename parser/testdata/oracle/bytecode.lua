-- Reference bytecode for the tests of the code generator, produced by
-- Tarantool's LuaJIT.
--
-- Usage: luajit bytecode.lua <dir with jit/vmdef.lua> <file>...
--
-- For each file prints "== <file>" and then either "error <message>" if
-- loadstring() fails, or "ok" followed by one line per prototype, the main
-- chunk first, each prototype followed by the prototypes of its constants:
--
--   <params> <framesize> <vararg> <upvalues> | <ins>... | <num>... | <gc>...
--
-- The instructions are hexadecimal, from the header at pc 0. The number
-- constants are printed with %.17g, the GC constants as "s:<string>",
-- "proto", "table" or "cdata". Control characters, bytes 128-255,
-- backslashes and spaces in strings are printed as \ddd.
package.path = arg[1] .. "/?.lua;" .. package.path

local bit = require("bit")
local jutil = require("jit.util")

local function escape(s, pattern)
    return (s:gsub(pattern or "[%c\\\128-\255]", function(c)
        return string.format("\\%03d", c:byte())
    end))
end

local function number(n)
    if n == math.huge then
        return "inf"
    elseif n == -math.huge then
        return "-inf"
    end
    return string.format("%.17g", n)
end

local function walk(fn, out)
    local info = jutil.funcinfo(fn)
    local parts = {
        info.params, info.stackslots, info.isvararg and "v" or "f",
        info.upvalues, "|",
    }
    for pc = 0, info.bytecodes - 1 do
        parts[#parts + 1] = bit.tohex((jutil.funcbc(fn, pc)))
    end
    parts[#parts + 1] = "|"
    for i = 0, info.nconsts - 1 do
        parts[#parts + 1] = number(jutil.funck(fn, i))
    end
    parts[#parts + 1] = "|"
    local children = {}
    for i = 1, info.gcconsts do
        local k = jutil.funck(fn, -i)
        if type(k) == "string" then
            parts[#parts + 1] = "s:" .. escape(k, "[%c%s\\\128-\255]")
        else
            parts[#parts + 1] = type(k)
            if type(k) == "proto" then
                children[#children + 1] = k
            end
        end
    end
    out[#out + 1] = table.concat(parts, " ")
    for _, child in ipairs(children) do
        walk(child, out)
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
