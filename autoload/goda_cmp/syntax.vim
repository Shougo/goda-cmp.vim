" Syntax helpers, as ddc#syntax#*(): for Config.DynamicConfig (through
" h.Call) and for Vim script conditions such as enabledIf.

" The syntax groups at the cursor. In a buffer without 'syntax', Neovim's
" tree-sitter captures.
function! goda_cmp#syntax#get() abort
  let curpos = s:pos()
  if &l:syntax !=# ''
    return s:syn_names(curpos)
  endif
  if !has('nvim')
    return []
  endif
  try
    return map(v:lua.vim.treesitter.get_captures_at_pos(
          \   0, curpos[0] - 1, curpos[1] - 1), 'v:val.capture')
  catch
    return []
  endtry
endfunction

" Is the cursor in one of the syntax groups {checks} (a name or a list)?
function! goda_cmp#syntax#in(checks) abort
  let names = goda_cmp#syntax#get()
  for check in type(a:checks) == v:t_list ? a:checks : [a:checks]
    if index(names, check) >= 0
      return v:true
    endif
  endfor
  return v:false
endfunction

" The language at the cursor: Neovim's tree-sitter, else 'filetype'
" (contextFiletype "treesitter").
function! goda_cmp#syntax#lang() abort
  if &l:filetype ==# '' || !has('nvim')
    return &l:filetype
  endif
  let curpos = s:pos()
  try
    " vim.treesitter.get_parser() fails without a parser. The injected
    " languages are known only after the lines are parsed (highlighting
    " parses them too, and parse() reuses that).
    return luaeval('(function(a) local p = vim.treesitter.get_parser() '
          \ .. 'p:parse({a[1] - 1, a[1]}) '
          \ .. 'return p:language_for_range({a[1] - 1, a[2] - 1, a[1] - 1, a[2] - 1}):lang() end)(_A)', curpos)
  catch
  endtry
  return ''
endfunction

function! s:pos() abort
  let curpos = getcurpos()[1:2]
  if mode() ==# 'i'
    let curpos[1] = max([1, curpos[1] - 1])
  endif
  return curpos
endfunction

function! s:syn_names(curpos) abort
  if col('$') >= 200
    return []
  endif
  let names = []
  try
    " synstack() seems broken in concealed text.
    for id in synstack(a:curpos[0], a:curpos[1])
      let name = synIDattr(id, 'name')
      call add(names, name)
      let trans = synIDattr(synIDtrans(id), 'name')
      if trans !=# name
        call add(names, trans)
      endif
    endfor
  catch
  endtry
  return names
endfunction
