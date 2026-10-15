" goda-cmp.vim - completion for Vim/Neovim on goda.vim
"
" The Go side (the user's own build of config.go, see README) gathers and
" filters candidates; this file collects the context while typing and shows
" the answer with the native popup or with pum.vim, as the Go side decides.

let s:seq = 0
let s:timer = -1
let s:last = []        " [bufnr, lnum, input] of the last request
let s:opts = {}        " per filetype: answer of cmp.options

" ---------------------------------------------------------------------------
" Setup

function! s:default_bin() abort
  return get(g:, 'goda_cmp_binary', s:config_dir() . '/goda-cmp' . (has('win32') ? '.exe' : ''))
endfunction

function! s:config_dir() abort
  if exists('g:goda_cmp_config_dir')
    return expand(g:goda_cmp_config_dir)
  endif
  return (has('nvim') ? stdpath('config') : expand('~/.vim')) . '/goda-cmp'
endfunction

" Build the user's config (a Go module in g:goda_cmp_config_dir). Needs Go.
function! goda_cmp#build() abort
  let l:dir = s:config_dir()
  if !isdirectory(l:dir)
    throw 'goda-cmp: no config directory: ' . l:dir
  endif
  let l:out = system('cd ' . shellescape(l:dir) . ' && go build -o ' . shellescape(s:default_bin()) . ' .')
  if v:shell_error
    throw 'goda-cmp: go build failed:' . "\n" . l:out
  endif
  echomsg 'goda-cmp: built ' . s:default_bin()
endfunction

" Start the plugin binary and begin completing in insert mode.
function! goda_cmp#enable() abort
  if exists('s:enabled')
    return
  endif
  call goda#start(s:default_bin())
  let s:enabled = 1
  augroup goda_cmp
    autocmd!
    autocmd TextChangedI * call s:on_change('TextChangedI')
    autocmd TextChangedP * call s:on_change('TextChangedP')
    autocmd InsertLeave * call goda_cmp#hide()
    autocmd BufEnter,FileType * let s:opts = {}
  augroup END
endfunction

function! goda_cmp#disable() abort
  if !exists('s:enabled')
    return
  endif
  unlet s:enabled
  silent! autocmd! goda_cmp
  call goda_cmp#hide()
  call goda#stop()
endfunction

" ---------------------------------------------------------------------------
" Requests

function! s:options() abort
  let l:ft = &filetype
  if !has_key(s:opts, l:ft)
    try
      let s:opts[l:ft] = goda#request('cmp', 'options', l:ft)
    catch
      return {'autoComplete': 0, 'delay': 0, 'ui': 'native'}
    endtry
  endif
  return s:opts[l:ft]
endfunction

function! s:context(manual) abort
  let s:seq += 1
  return {
        \ 'seq': s:seq,
        \ 'tick': b:changedtick,
        \ 'bufnr': bufnr('%'),
        \ 'lnum': line('.'),
        \ 'col': col('.'),
        \ 'input': strpart(getline('.'), 0, col('.') - 1),
        \ 'filetype': &filetype,
        \ 'manual': a:manual ? v:true : v:false,
        \ }
endfunction

function! s:send(manual) abort
  let s:timer = -1
  if mode() !~# '^i' || !goda#is_running()
    return
  endif
  call goda#notify('cmp', 'complete', s:context(a:manual))
endfunction

function! s:on_change(event) abort
  if !goda#is_running()
    return
  endif
  let l:opts = s:options()
  " Text that the popup itself inserted (selecting an item) must not start a
  " new completion.
  if get(l:opts, 'ui', '') ==# 'pum' && pum#skip_complete()
    return
  endif
  if a:event ==# 'TextChangedP' && get(complete_info(['selected']), 'selected', -1) >= 0
    return
  endif
  if !get(l:opts, 'autoComplete', 1)
    return
  endif
  let l:key = [bufnr('%'), line('.'), strpart(getline('.'), 0, col('.') - 1)]
  if l:key ==# s:last
    return
  endif
  let s:last = l:key

  if s:timer != -1
    call timer_stop(s:timer)
  endif
  let s:timer = timer_start(get(l:opts, 'delay', 0), {-> s:send(0)})
endfunction

" Complete now, whatever the typed length. Meant for a mapping:
"   inoremap <C-n> <Cmd>call goda_cmp#complete()<CR>
function! goda_cmp#complete() abort
  let s:last = []
  call s:send(1)
  return ''
endfunction

function! goda_cmp#hide() abort
  if s:timer != -1
    call timer_stop(s:timer)
    let s:timer = -1
  endif
  let s:last = []
  if goda#is_running()
    try
      call goda#notify('cmp', 'hide', &filetype)
    catch
    endtry
  endif
endfunction

" ---------------------------------------------------------------------------
" UIs, called BY the plugin binary. An answer is dropped when a newer request
" was made or the text changed in the meantime.

function! s:fresh(r) abort
  return a:r.seq == s:seq && a:r.tick == b:changedtick && mode() =~# '^i'
endfunction

function! goda_cmp#_native_show(r) abort
  if !s:fresh(a:r)
    return 0
  endif
  call complete(a:r.col, a:r.items)
  return 1
endfunction

function! goda_cmp#_native_hide() abort
  if mode() =~# '^i' && pumvisible()
    call complete(col('.'), [])
  endif
endfunction

function! goda_cmp#_pum_show(r) abort
  if !s:fresh(a:r)
    return 0
  endif
  call pum#open(a:r.col, a:r.items)
  return 1
endfunction

function! goda_cmp#_pum_hide() abort
  if exists('*pum#visible') && pum#visible()
    call pum#close()
  endif
endfunction
