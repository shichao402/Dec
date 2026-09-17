import { describe, expect, it } from 'vitest'
import { suggestProjectName } from './console'

// 私仓项目名契约见 internal/types/pack.go 的 ProjectNamePattern。
const projectName = /^[a-z0-9]+(?:-[a-z0-9]+)*$/

describe('suggestProjectName', () => {
  it('keeps camel-case word boundaries readable', () => {
    expect(suggestProjectName('D:\\workspace\\CppSvnAuthorAnalysis')).toBe('cpp-svn-author-analysis')
  })

  it('splits an acronym from the word that follows it', () => {
    expect(suggestProjectName('/home/me/HTTPServerTools')).toBe('http-server-tools')
  })

  it('folds separators and trims the leftovers', () => {
    expect(suggestProjectName('/home/me/my_project.v2/')).toBe('my-project-v2')
    expect(suggestProjectName('/home/me/__agents help me__')).toBe('agents-help-me')
  })

  it('only ever suggests a valid project name', () => {
    for (const root of [
      'D:\\workspace\\CppSvnAuthorAnalysis',
      '/home/me/HTTPServerTools',
      '/home/me/my_project.v2/',
      '/home/me/__agents help me__',
      '/home/me/dec',
    ]) {
      expect(suggestProjectName(root)).toMatch(projectName)
    }
  })
})
