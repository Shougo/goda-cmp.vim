" goda-cmp.vim - completion for Vim/Neovim on goda.vim
"
" The Go side (the user's own build of config.go, see README) gathers and
" filters candidates; this file collects the context while typing and shows
" the answer with the native popup or with pum.vim, as the Go side decides.

let s:seq = 0
let s:timer = -1
let s:last = []        " [bufnr, lnum, input] of the last request
let s:opts = {}        " answers of cmp.options, by filetype and patches

" Settings made at run time. They live here, not in the plugin binary: they
" survive a restart of the binary, buffer settings go away with the buffer, and
" they can be made before the binary is started. Every request carries them.
let s:global_patch = {}
let s:filetype_patch = {}

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

" Stop, build and start again: what to run after the config or an extension
" changed. Meant for an autocmd, see the README.
function! goda_cmp#rebuild() abort
  call goda_cmp#disable()
  call goda_cmp#build()
  call goda_cmp#enable()
endfunction

" Start the plugin binary and begin completing in insert mode.
function! goda_cmp#enable() abort
  if exists('s:enabled')
    return
  endif
  if !executable(s:default_bin())
    " Not an error: this is what the first start looks like.
    echohl WarningMsg
    echomsg 'goda-cmp: ' . s:default_bin() . ' does not exist yet; build it with: call goda_cmp#build()'
    echohl None
    return
  endif
  call goda#start(s:default_bin())
  if !goda#is_running('cmp')
    call goda#stop()
    throw 'goda-cmp: ' . s:default_bin() . ' does not contain the "cmp" plugin'
  endif
  let s:enabled = 1
  augroup goda_cmp
    autocmd!
    autocmd TextChangedI * call s:on_change('TextChangedI')
    autocmd TextChangedP * call s:on_change('TextChangedP')
    autocmd InsertLeave * call goda_cmp#hide()
  augroup END
endfunction

function! goda_cmp#disable() abort
  if !exists('s:enabled')
    return
  endif
  unlet s:enabled
  silent! autocmd! goda_cmp
  call goda_cmp#hide()
  call goda#stop('cmp')
endfunction

" ---------------------------------------------------------------------------
" Requests

" ---------------------------------------------------------------------------
" Settings (ddc.vim: ddc#custom#patch_*). A patch is a dictionary with the
" keys of cmp.Config (ui, sources, sourceOptions, ...). Maps are merged by
" key, everything else is replaced. A patch is checked by the plugin when it
" is running, and an invalid one throws.

function! s:merge(base, patch) abort
  let l:r = deepcopy(a:base)
  for [l:key, l:val] in items(a:patch)
    if l:key ==# 'sourceOptions' && type(l:val) == v:t_dict
      let l:opts = get(l:r, 'sourceOptions', {})
      for [l:src, l:opt] in items(l:val)
        let l:opts[l:src] = extend(get(l:opts, l:src, {}), deepcopy(l:opt))
      endfor
      let l:r.sourceOptions = l:opts
    else
      let l:r[l:key] = deepcopy(l:val)
    endif
  endfor
  return l:r
endfunction

function! s:check(config) abort
  if goda#is_running('cmp')
    call goda#request('cmp', 'validate', a:config)
  endif
endfunction

function! goda_cmp#patch_global(patch) abort
  call s:check(a:patch)
  let s:global_patch = s:merge(s:global_patch, a:patch)
endfunction

function! goda_cmp#patch_filetype(filetype, patch) abort
  call s:check(a:patch)
  let s:filetype_patch[a:filetype] = s:merge(get(s:filetype_patch, a:filetype, {}), a:patch)
endfunction

function! goda_cmp#patch_buffer(patch) abort
  call s:check(a:patch)
  let b:goda_cmp_patch = s:merge(get(b:, 'goda_cmp_patch', {}), a:patch)
endfunction

function! goda_cmp#get_global() abort
  return deepcopy(s:global_patch)
endfunction

function! goda_cmp#get_filetype(filetype) abort
  return deepcopy(get(s:filetype_patch, a:filetype, {}))
endfunction

function! goda_cmp#get_buffer() abort
  return deepcopy(get(b:, 'goda_cmp_patch', {}))
endfunction

function! goda_cmp#set_global(config) abort
  call s:check(a:config)
  let s:global_patch = deepcopy(a:config)
endfunction

function! goda_cmp#set_filetype(filetype, config) abort
  call s:check(a:config)
  let s:filetype_patch[a:filetype] = deepcopy(a:config)
endfunction

function! goda_cmp#set_buffer(config) abort
  call s:check(a:config)
  let b:goda_cmp_patch = deepcopy(a:config)
endfunction

" What the plugin needs to resolve the settings of this buffer.
function! s:patches() abort
  let l:fts = {}
  for l:part in split(&filetype, '\.')
    if has_key(s:filetype_patch, l:part)
      let l:fts[l:part] = s:filetype_patch[l:part]
    endif
  endfor
  return {'global': s:global_patch, 'filetype': l:fts, 'buffer': get(b:, 'goda_cmp_patch', {})}
endfunction

" ---------------------------------------------------------------------------
" Requests

function! s:options() abort
  let l:patches = s:patches()
  let l:key = &filetype . "\n" . json_encode(l:patches)
  if !has_key(s:opts, l:key)
    if len(s:opts) > 50
      let s:opts = {}
    endif
    try
      let s:opts[l:key] = goda#request('cmp', 'options', &filetype, l:patches)
    catch
      return {'autoComplete': 0, 'delay': 0, 'ui': 'native'}
    endtry
  endif
  return s:opts[l:key]
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
        \ 'mode': 'i',
        \ 'manual': a:manual ? v:true : v:false,
        \ 'patches': s:patches(),
        \ }
endfunction

function! s:send(manual) abort
  let s:timer = -1
  if mode() !~# '^i' || !goda#is_running('cmp')
    return
  endif
  call goda#notify('cmp', 'complete', s:context(a:manual))
endfunction

function! s:on_change(event) abort
  if !goda#is_running('cmp')
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
  if goda#is_running('cmp')
    try
      call goda#notify('cmp', 'hide', &filetype, s:patches())
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
