" The Vim script part of examples/ddc: the hooks of a ddc.vim configuration,
" translated. Use it with g:goda_cmp#config_dir pointing to a copy of this
" directory.

" Command line {{{
nnoremap :
      \ <Cmd>call CommandlinePre(':')<CR>:
nnoremap ?
      \ <Cmd>call CommandlinePre('/')<CR>?
xnoremap :
      \ <Cmd>call CommandlinePre(':')<CR>:

function CommandlinePre(mode) abort
  if 'b:prev_buffer_config'->exists()
    return
  endif

  " Overwrite sources
  let b:prev_buffer_config = goda_cmp#get_buffer()

  if a:mode ==# ':'
    call goda_cmp#patch_buffer(#{
          \   sourceOptions: #{
          \     _: #{
          \       keywordPattern: '(~\w+)?[0-9a-zA-Z_:#*/.-]*',
          \     },
          \   },
          \ })
  endif

  autocmd User GodaCmpCmdlineLeave ++once call CommandlinePost()

  call goda_cmp#enable_cmdline()
endfunction
function CommandlinePost() abort
  " Restore config
  if 'b:prev_buffer_config'->exists()
    call goda_cmp#set_buffer(b:prev_buffer_config)
    unlet b:prev_buffer_config
  endif
endfunction
" }}}

" pum.vim {{{
call pum#set_option(#{
      \   auto_select: v:false,
      \   commit_characters: ['.'],
      \   insert_preview: v:true,
      \   max_height: 5,
      \   max_width: 80,
      \   preview: v:true,
      \   preview_remains: v:true,
      \   preview_width: 80,
      \   previewer: 'goda_cmp#get_previewer',
      \   use_setline: !has('nvim'),
      \ })
" }}}

" Insert mode {{{
inoremap <S-Tab> <Cmd>call pum#map#insert_relative(-1, 'empty')<CR>
inoremap <C-n>   <Cmd>call pum#map#select_relative(+1)<CR>
inoremap <C-p>   <Cmd>call pum#map#select_relative(-1)<CR>
inoremap <C-y>   <Cmd>call pum#map#confirm_suffix()<CR>
inoremap <C-o>   <Cmd>call pum#map#confirm_matched_pattern('^\S\+')<CR>
inoremap <Home>  <Cmd>call pum#map#insert_relative(-9999, 'ignore')<CR>
inoremap <End>   <Cmd>call pum#map#insert_relative(+9999, 'ignore')<CR>
inoremap <C-t>   <C-v><Tab>

inoremap <expr> <C-g>   goda_cmp#map#insert_item(0)
cnoremap <expr> <C-g>   goda_cmp#map#insert_item(0)

inoremap <expr> <TAB>
      \   goda_cmp#ui#inline#visible()
      \ ? goda_cmp#map#insert_item(0)
      \ : pum#visible()
      \ ? '<Cmd>call pum#map#insert_relative(+1, "empty")<CR>'
      \ : col('.') <= 1 ? '<TAB>'
      \ : getline('.')[col('.') - 2] =~# '\s'
      \ ? '<TAB>'
      \ : goda_cmp#map#manual_complete()
inoremap <expr> <C-e>
      \   goda_cmp#map#can_complete()
      \ ? goda_cmp#map#insert_item(0)
      \ : '<End>'
inoremap <expr> <C-l>  goda_cmp#map#manual_complete()

" Mouse support
inoremap <LeftMouse>  <Cmd>call pum#map#confirm_mouse()<CR>
inoremap <RightMouse>  <Cmd>call pum#map#select_mouse()<CR>
cnoremap <LeftMouse>  <Cmd>call pum#map#confirm_mouse()<CR>
cnoremap <RightMouse>  <Cmd>call pum#map#select_mouse()<CR>
tnoremap <LeftMouse>  <Cmd>call pum#map#confirm_mouse()<CR>
tnoremap <RightMouse>  <Cmd>call pum#map#select_mouse()<CR>
" }}}

" Command-line mode {{{
cnoremap <S-Tab> <Cmd>call pum#map#insert_relative(-1)<CR>
cnoremap <C-o>   <Cmd>call pum#map#confirm()<CR>
cnoremap <C-q>   <Cmd>call pum#map#select_relative(+1)<CR>
cnoremap <C-z>   <Cmd>call pum#map#select_relative(-1)<CR>
cnoremap <C-y>   <Cmd>call pum#map#confirm()<CR>

cnoremap <expr> <Tab>
      \   goda_cmp#ui#inline#visible()
      \ ? goda_cmp#map#insert_item(0)
      \ : wildmenumode()
      \ ? &wildcharm->nr2char()
      \ : pum#visible()
      \ ? '<Cmd>call pum#map#insert_relative(+1)<CR>'
      \ : goda_cmp#map#manual_complete()
cnoremap <expr> <C-e>
      \   goda_cmp#ui#inline#visible()
      \ ? goda_cmp#map#insert_item(0)
      \ : pum#visible()
      \ ? '<Cmd>call pum#map#cancel()<CR>'
      \ : '<End>'

xnoremap <Tab>   "_R<Cmd>call goda_cmp#map#manual_complete()<CR>
snoremap <Tab>   <C-o>"_di<Cmd>call goda_cmp#map#manual_complete()<CR>
" }}}

" ddc#enable(#{context_filetype: ...}) is the contextFiletype option.
call goda_cmp#patch_global(#{
      \   contextFiletype: has('nvim') ? 'treesitter' : 'none',
      \ })

" Terminal completion
call goda_cmp#enable_terminal()
