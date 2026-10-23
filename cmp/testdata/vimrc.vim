" vimrc of the real-Vim end-to-end test (cmp/e2e_test.go).
set nocompatible noswapfile nobackup nowritebackup ttimeout ttimeoutlen=10
" Neovim's defaults differ from Vim's: keep what is typed as it is typed.
set noautoindent nosmarttab
" $GODA_COMPLETEOPT lets a test run with other completeopt values.
let &completeopt = empty($GODA_COMPLETEOPT) ? 'menuone,noinsert,noselect' : $GODA_COMPLETEOPT
let &runtimepath = $GODA_RTP . ',' . $GODA_CMP_RTP . (empty($PUM_RTP) ? '' : ',' . $PUM_RTP) . ',' . &runtimepath
let g:goda_cmp#binary = $GODA_CMP_BIN
filetype plugin on
" Neovim turns indenting on by default (Vim does not).
filetype indent off

" Ctrl-] in insert mode records what the popup shows.
function! DumpPopup() abort
  let ui = empty($PUM_RTP) ? 'native' : 'pum'
  if ui ==# 'pum'
    let visible = pum#visible()
    let words = visible ? map(copy(pum#_get().items), 'v:val.word') : []
  else
    let visible = pumvisible()
    " What the user sees: complete_info().items comes back in reverse order
    " (checked in Vim 9.1), so read the popup rows from the screen instead.
    " pum_getpos() says where the popup is (it may open above the cursor)
    " and how many rows it shows (fewer than the items when they do not fit).
    let words = []
    if visible
      " Neovim answers with Floats.
      let pos = map(pum_getpos(), {_, v -> type(v) == v:t_float ? float2nr(v) : v})
      for i in range(pos.height)
        let row = join(map(range(pos.col + 1, pos.col + pos.width),
              \ {_, c -> screenstring(pos.row + 1 + i, c)}), '')
        call add(words, split(trim(row))[0])
      endfor
    endif
  endif
  " pum.vim's preview window: the 'buftype' of the buffer it shows ("help"
  " for a help previewer), "" when it is closed. (In Neovim the help buffer
  " is not pum#get_preview_buf().)
  let preview = ui ==# 'pum' && exists('*pum#preview_visible') && pum#preview_visible()
        \ ? getbufvar(winbufnr(pum#_get().preview_id), '&buftype', '?') : ''
  " In a terminal, the line of the terminal's cursor (Vim keeps it apart).
  let line = mode() ==# 't' && !has('nvim')
        \ ? term_getline('', term_getcursor(bufnr('%'))[0]) : getline('.')
  let inline = exists('*goda_cmp#ui#inline#visible') && goda_cmp#ui#inline#visible()
  call writefile([json_encode({'visible': visible ? v:true : v:false, 'words': words, 'line': line,
        \ 'inline': inline ? v:true : v:false,
        \ 'cmdline': mode() ==# 'c' ? getcmdline() : '', 'preview': preview})], $GODA_OUT, 'a')
  return ''
endfunction
inoremap <C-]> <Cmd>call DumpPopup()<CR>
cnoremap <C-]> <Cmd>call DumpPopup()<CR>
tnoremap <C-]> <Cmd>call DumpPopup()<CR>

" $GODA_FUNCS: the functions for mappings, aliases and params from Vim script,
" and contextFiletype.
if !empty($GODA_FUNCS)
  inoremap <expr> <C-x>1 goda_cmp#map#insert_item(0)
  inoremap <expr> <C-x>2 goda_cmp#map#complete_common_string()
  function! DumpCurrent() abort
    let cur = goda_cmp#get_current()
    call writefile([json_encode({'current': {
          \ 'mark': get(get(cur.sourceOptions, 'around', {}), 'mark', ''),
          \ 'visible': goda_cmp#visible()}})], $GODA_OUT, 'a')
    return ''
  endfunction
  inoremap <C-x>3 <Cmd>call DumpCurrent()<CR>
  " around2: around with params of its own, its duplicates kept.
  call goda_cmp#alias('source', 'around2', 'around')
  call goda_cmp#patch_global({
        \ 'sources': ['around', 'around2'],
        \ 'sourceParams': {'around2': {'minLength': 9}},
        \ 'sourceOptions': {'around2': {'dup': 'keep'}},
        \ 'contextFiletype': 'treesitter',
        \ })
  call goda_cmp#patch_filetype('lua', {'sourceOptions': {'around': {'mark': 'LUA'}}})
endif

" $GODA_PORTS: the ported extensions, as a ddc.vim user maps them.
if !empty($GODA_PORTS)
  inoremap <expr> <C-g> goda_cmp#map#insert_item(0)
  cnoremap <expr> <C-g> goda_cmp#map#insert_item(0)
  let @a = 'registerword_qq other'
  call pum#set_option('auto_select', v:false)
  " Terminal buffers: the shell's own completion.
  execute 'autocmd' (has('nvim') ? 'TermOpen' : 'TerminalOpen') '*'
        \ 'call goda_cmp#patch_buffer(#{specialBufferCompletion: v:true, sources: ["shellnative"]})'
endif

" $GODA_TERMINAL: completion in a terminal, with pum.vim.
if !empty($GODA_TERMINAL)
  autocmd VimEnter * call goda_cmp#enable_terminal()
  call pum#set_option('auto_select', v:false)
  " pum#map#insert_relative() does nothing in a terminal: select, then
  " confirm.
  tnoremap <C-n> <Cmd>call pum#map#select_relative(+1)<CR>
  tnoremap <C-y> <Cmd>call pum#map#confirm()<CR>
  " (:terminal takes "|" as part of its command: a function.)
  function! GodaTerm() abort
    if has('nvim')
      terminal sh
      startinsert
    else
      terminal ++curwin sh
    endif
  endfunction
  command! GodaTerm call GodaTerm()
endif

" $GODA_CMDLINE: ":" enables command-line completion for that command line,
" as in ddc.vim's documentation, and its end is recorded.
if !empty($GODA_CMDLINE)
  nnoremap : <Cmd>call goda_cmp#enable_cmdline()<CR>:
  autocmd User GodaCmpCmdlineLeave
        \ call writefile([json_encode({'left': v:true})], $GODA_OUT, 'a')
  " With pum.vim itself (not the stand-in): select and accept as its
  " documentation does.
  if !empty($GODA_REAL_PUM)
    " The first Ctrl-N selects the first item.
    call pum#set_option('auto_select', v:false)
    " Previews, with a pum.vim that has the previewer option.
    if has_key(pum#_options(), 'previewer')
      call pum#set_option(#{preview: v:true, preview_delay: 10,
            \ previewer: 'goda_cmp#get_previewer'})
    endif
    cnoremap <C-n> <Cmd>call pum#map#insert_relative(+1)<CR>
    cnoremap <C-y> <Cmd>call pum#map#confirm()<CR>
  endif
endif

" The pum scenario switches the popup from Vim script.
if !empty($PUM_RTP)
  call goda_cmp#patch_global({'ui': 'pum'})
endif
autocmd VimEnter * call goda_cmp#enable()

" Run-time settings, as in a ddc.vim configuration that patches the buffer
" while the command line is used: Ctrl-G switches the sources off for this
" buffer, Ctrl-T puts the previous settings back, Ctrl-Y tries an invalid patch
" and records the error.
function! PatchOff() abort
  let g:prev_config = goda_cmp#get_buffer()
  call goda_cmp#patch_buffer({'sources': []})
  return ''
endfunction
function! PatchRestore() abort
  call goda_cmp#set_buffer(g:prev_config)
  return ''
endfunction
function! PatchBad() abort
  try
    call goda_cmp#patch_buffer({'sources': ['no_such_source']})
    let err = 'no error'
  catch
    let err = v:exception
  endtry
  call writefile([json_encode({'error': err, 'buffer': goda_cmp#get_buffer()})], $GODA_OUT, 'a')
  return ''
endfunction
" $GODA_NO_PATCH_MAPS leaves the keys alone (Ctrl-Y accepts a candidate).
if empty($GODA_NO_PATCH_MAPS)
  inoremap <C-g> <Cmd>call PatchOff()<CR>
  inoremap <C-t> <Cmd>call PatchRestore()<CR>
  inoremap <C-y> <Cmd>call PatchBad()<CR>
endif
