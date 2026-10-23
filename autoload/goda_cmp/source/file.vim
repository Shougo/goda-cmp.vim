" The Vim side of ext/source/file: what is before the cursor, in the terms of
" 'isfname' and 'iskeyword'. A port of the Vim script of ddc-source-file
" (https://github.com/LumaKernel/ddc-source-file, MIT License, Copyright (c)
" 2021 Luma). Called only by that source.

function! s:line_to_file_full(line, is_posix) abort
  let separator = a:is_posix ? '/' : '\\'
  let pattern = printf('\c\%%(^\$\%%(env:\)\?\)\?[[:fname:]%s]*$', separator)
  return matchlist(a:line, pattern)[0]
endfunction

function! s:full_to_base_prefix(full, chars, is_posix) abort
  let separator = a:is_posix ? '/' : '\'
  let separator2 = a:is_posix ? '/' : '\\'
  let pattern = printf('\%%(.*\%s\|^\)\(.*[^%s%s]\)[%s]*$',
        \ separator, a:chars, separator2, a:chars)
  return get(matchlist(a:full, pattern), 1, '')
endfunction

" @param {string} input_line
" @param {string} chars
" @param {boolean} is_posix
" @return {[
"   string,
"   string,
"   string,
" ]}
function! goda_cmp#source#file#info(input_line, chars, is_posix) abort
  let input_file_full = s:line_to_file_full(a:input_line, a:is_posix)
  let input_file_base_prefix = s:full_to_base_prefix(
        \ input_file_full, a:chars, a:is_posix)
  let buf_path = expand('%:p')
  return [
        \ input_file_full,
        \ input_file_base_prefix,
        \ buf_path,
        \ ]
endfunction

function! goda_cmp#source#file#get_pos(input_line, chars) abort
  return match(a:input_line, printf('[%s]*$', a:chars))
endfunction
