" The Vim side of the extensions' end-to-end tests, sourced by the vimrc of
" package e2e ($GODA_VIMRC).

" What the UIs show: pum.vim's popup when $PUM_RTP is set, Vim's own
" otherwise, the 'buftype' of pum.vim's preview window ("help" for a help
" previewer, "" when it is closed; in Neovim the help buffer is not
" pum#get_preview_buf()), and whether the inline UI shows a candidate.
function! GodaDumpUI() abort
  if empty($PUM_RTP)
    let d = GodaDumpNative()
    let preview = ''
  else
    let visible = pum#visible()
    let d = {'visible': visible ? v:true : v:false,
          \ 'words': visible ? map(copy(pum#_get().items), 'v:val.word') : []}
    let preview = exists('*pum#preview_visible') && pum#preview_visible()
          \ ? getbufvar(winbufnr(pum#_get().preview_id), '&buftype', '?') : ''
  endif
  let inline = exists('*goda_cmp#ui#inline#visible') && goda_cmp#ui#inline#visible()
  return extend(d, {'preview': preview, 'inline': inline ? v:true : v:false})
endfunction

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
