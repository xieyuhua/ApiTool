package doc

// .docx 包结构：包清单、关系、样式等静态部件的集中维护处，以及最终打包。
// 正文内容渲染在 docx.go，页眉水印在 watermark_vml.go。

import (
	"archive/zip"
	"bytes"
	"fmt"
	"time"

	"apitool/internal/model"
)

// BuildDOCX 生成 Word 文档（.docx）字节流，按配置写入页眉文本水印。
func BuildDOCX(title, rootID string, dirs []model.Directory, apis []model.ApiInfo, common model.CommonParams, wm model.DocWatermark) ([]byte, error) {
	return BuildDOCXWithLayout(title, rootID, dirs, apis, common, wm, model.DocLayout{})
}

// BuildDOCXWithLayout 生成带企业文档排版（封面 / 目录 / 前言 / 规范 / 附录 / 修订记录）的 .docx。
func BuildDOCXWithLayout(title, rootID string, dirs []model.Directory, apis []model.ApiInfo, common model.CommonParams, wm model.DocWatermark, layout model.DocLayout) ([]byte, error) {
	body := buildDocxBody(title, rootID, dirs, apis, common, layout)
	return packDOCX(title, body, docxWatermarkMarkup(wm))
}

// packDOCX 把正文与页眉（水印）装配成完整的 .docx 包。
// 附带 settings.xml：打开时提示更新域，使 TOC 目录能自动生成页码。
func packDOCX(title, body, watermarkMarkup string) ([]byte, error) {
	return zipParts([]docxPart{
		{"[Content_Types].xml", docxContentTypes},
		{"_rels/.rels", docxRootRels},
		{"docProps/core.xml", docxCoreProps(title)},
		{"docProps/app.xml", docxAppProps},
		{"word/document.xml", docxDocumentXML(body)},
		{"word/styles.xml", docxStyles},
		{"word/settings.xml", docxSettings},
		{"word/header1.xml", docxHeaderXML(watermarkMarkup)},
		{"word/_rels/document.xml.rels", docxDocRels},
	})
}

// docxPart 一个包内部件（路径 + XML 内容）。
type docxPart struct{ name, body string }

// zipParts 把部件写入 zip 容器（docx 本质是 zip 包）。
func zipParts(parts []docxPart) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, p := range parts {
		w, err := zw.Create(p.name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(p.body)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// docxDocumentXML 包装正文 XML，并声明页面尺寸、页边距与默认页眉。
func docxDocumentXML(body string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"` +
		` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"` +
		` xmlns:v="urn:schemas-microsoft-com:vml"` +
		` xmlns:o="urn:schemas-microsoft-com:office:office"` +
		` xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"` +
		` xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math">` +
		`<w:body>` + body +
		`<w:sectPr><w:headerReference w:type="default" r:id="rId2"/>` +
		fmt.Sprintf(`<w:pgSz w:w="%d" w:h="%d"/>`, docxPageWidth, docxPageHeight) +
		fmt.Sprintf(`<w:pgMar w:top="%d" w:right="%d" w:bottom="%d" w:left="%d" w:header="851" w:footer="992" w:gutter="0"/>`,
			docxMarginY, docxMarginX, docxMarginY, docxMarginX) +
		`<w:cols w:space="425"/><w:docGrid w:linePitch="312"/></w:sectPr>` +
		`</w:body></w:document>`
}

// docxHeaderXML 包装页眉部件（watermarkMarkup 为空即无水印的空白页眉）。
func docxHeaderXML(watermarkMarkup string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
 xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"
 xmlns:v="urn:schemas-microsoft-com:vml"
 xmlns:o="urn:schemas-microsoft-com:office:office"
 xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing">
<w:p><w:pPr><w:spacing w:before="0" w:after="0"/></w:pPr>` + watermarkMarkup + `</w:p>
</w:hdr>`
}

// docxCoreProps 生成文档属性（标题 / 创建者 / 时间）。
func docxCoreProps(title string) string {
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties"` +
		` xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/"` +
		` xmlns:dcmitype="http://purl.org/dc/dcmitype/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">` +
		`<dc:title>` + xmlEscape(title) + `</dc:title><dc:creator>ApiTool</dc:creator>` +
		`<cp:lastModifiedBy>ApiTool</cp:lastModifiedBy>` +
		`<dcterms:created xsi:type="dcterms:W3CDTF">` + now + `</dcterms:created>` +
		`<dcterms:modified xsi:type="dcterms:W3CDTF">` + now + `</dcterms:modified>` +
		`</cp:coreProperties>`
}

// docxTableBorders 表格边框（所有框线统一浅灰细线）。
const docxTableBorders = `<w:tblBorders>` +
	`<w:top w:val="single" w:sz="4" w:space="0" w:color="E5E6EB"/>` +
	`<w:left w:val="single" w:sz="4" w:space="0" w:color="E5E6EB"/>` +
	`<w:bottom w:val="single" w:sz="4" w:space="0" w:color="E5E6EB"/>` +
	`<w:right w:val="single" w:sz="4" w:space="0" w:color="E5E6EB"/>` +
	`<w:insideH w:val="single" w:sz="4" w:space="0" w:color="E5E6EB"/>` +
	`<w:insideV w:val="single" w:sz="4" w:space="0" w:color="E5E6EB"/>` +
	`</w:tblBorders>`

// docxTableCellMargin 单元格内边距。
const docxTableCellMargin = `<w:tblCellMar>` +
	`<w:top w:w="60" w:type="dxa"/><w:left w:w="100" w:type="dxa"/>` +
	`<w:bottom w:w="60" w:type="dxa"/><w:right w:w="100" w:type="dxa"/>` +
	`</w:tblCellMar>`

const docxContentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
<Override PartName="/word/settings.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml"/>
<Override PartName="/word/header1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml"/>
<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>
<Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/>
</Types>`

const docxRootRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>
<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/>
</Relationships>`

const docxDocRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/>
<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings" Target="settings.xml"/>
</Relationships>`

const docxAppProps = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties" xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes">
<Application>ApiTool</Application>
</Properties>`

// docxStyles 文档样式表：正文默认字体、标题、代码块、置灰链接行等。
// 全部样式都以 Normal 为基础，保证 Word / WPS 中可用内置样式面板调整。
const docxStyles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:docDefaults><w:rPrDefault><w:rPr>
<w:rFonts w:ascii="Microsoft YaHei" w:hAnsi="Microsoft YaHei" w:eastAsia="Microsoft YaHei" w:cs="Microsoft YaHei"/>
<w:sz w:val="21"/><w:szCs w:val="21"/></w:rPr></w:rPrDefault>
<w:pPrDefault><w:pPr><w:spacing w:after="80" w:line="300" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults>
<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/></w:style>
<w:style w:type="paragraph" w:styleId="DocTitle"><w:name w:val="Doc Title"/><w:basedOn w:val="Normal"/><w:qFormat/>
<w:pPr><w:jc w:val="center"/><w:spacing w:before="240" w:after="120"/></w:pPr>
<w:rPr><w:b/><w:sz w:val="44"/><w:szCs w:val="44"/><w:color w:val="1F2329"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="DocMeta"><w:name w:val="Doc Meta"/><w:basedOn w:val="Normal"/><w:qFormat/>
<w:pPr><w:jc w:val="center"/><w:spacing w:before="0" w:after="280"/></w:pPr>
<w:rPr><w:sz w:val="18"/><w:szCs w:val="18"/><w:color w:val="86909C"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="DocSpacer"><w:name w:val="Doc Spacer"/><w:basedOn w:val="Normal"/>
<w:pPr><w:spacing w:before="0" w:after="0" w:line="200" w:lineRule="auto"/></w:pPr>
<w:rPr><w:sz w:val="10"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/>
<w:pPr><w:keepNext/><w:keepLines/><w:spacing w:before="360" w:after="160"/>
<w:pBdr><w:bottom w:val="single" w:sz="8" w:space="4" w:color="E5E6EB"/></w:pBdr><w:outlineLvl w:val="0"/></w:pPr>
<w:rPr><w:b/><w:sz w:val="32"/><w:szCs w:val="32"/><w:color w:val="1F2329"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/>
<w:pPr><w:keepNext/><w:keepLines/><w:spacing w:before="280" w:after="120"/><w:outlineLvl w:val="1"/></w:pPr>
<w:rPr><w:b/><w:sz w:val="26"/><w:szCs w:val="26"/><w:color w:val="1F2329"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading3"><w:name w:val="heading 3"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/>
<w:pPr><w:keepNext/><w:spacing w:before="200" w:after="80"/><w:outlineLvl w:val="2"/></w:pPr>
<w:rPr><w:b/><w:sz w:val="21"/><w:szCs w:val="21"/><w:color w:val="4E5969"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="DocURL"><w:name w:val="Doc Url"/><w:basedOn w:val="Normal"/>
<w:pPr><w:shd w:val="clear" w:color="auto" w:fill="F7F8FA"/><w:spacing w:before="40" w:after="140"/></w:pPr>
<w:rPr><w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:eastAsia="Consolas"/><w:sz w:val="19"/><w:szCs w:val="19"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="DocCode"><w:name w:val="Doc Code"/><w:basedOn w:val="Normal"/>
<w:pPr><w:shd w:val="clear" w:color="auto" w:fill="F7F8FA"/><w:spacing w:before="0" w:after="0" w:line="240" w:lineRule="auto"/></w:pPr>
<w:rPr><w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:eastAsia="Consolas"/><w:sz w:val="17"/><w:szCs w:val="17"/><w:color w:val="1F2329"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="DocCoverTitle"><w:name w:val="Doc Cover Title"/><w:basedOn w:val="Normal"/><w:qFormat/>
<w:pPr><w:jc w:val="center"/><w:spacing w:before="2400" w:after="240"/></w:pPr>
<w:rPr><w:b/><w:sz w:val="56"/><w:szCs w:val="56"/><w:color w:val="1F2329"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="DocCoverSubtitle"><w:name w:val="Doc Cover Subtitle"/><w:basedOn w:val="Normal"/>
<w:pPr><w:jc w:val="center"/><w:spacing w:before="0" w:after="600"/></w:pPr>
<w:rPr><w:sz w:val="26"/><w:szCs w:val="26"/><w:color w:val="4E5969"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="DocCoverLine"><w:name w:val="Doc Cover Line"/><w:basedOn w:val="Normal"/>
<w:pPr><w:jc w:val="center"/><w:spacing w:before="0" w:after="120"/></w:pPr>
<w:rPr><w:sz w:val="22"/><w:szCs w:val="22"/><w:color w:val="4E5969"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="DocBullet"><w:name w:val="Doc Bullet"/><w:basedOn w:val="Normal"/>
<w:pPr><w:spacing w:before="0" w:after="60"/><w:ind w:left="300"/></w:pPr></w:style>
<w:style w:type="paragraph" w:styleId="DocQuote"><w:name w:val="Doc Quote"/><w:basedOn w:val="Normal"/>
<w:pPr><w:shd w:val="clear" w:color="auto" w:fill="F7F8FA"/><w:pBdr><w:left w:val="single" w:sz="18" w:space="6" w:color="165DFF"/></w:pBdr>
<w:spacing w:before="80" w:after="80"/><w:ind w:left="180"/></w:pPr>
<w:rPr><w:color w:val="4E5969"/></w:rPr></w:style>
</w:styles>`

// docxSettings 文档级设置：打开时自动更新域，使 TOC 目录生成真实页码。
const docxSettings = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:zoom w:percent="100"/>
<w:defaultTabStop w:val="420"/>
<w:updateFields w:val="true"/>
<w:compat><w:compatSetting w:name="compatibilityMode" w:uri="http://schemas.microsoft.com/office/word" w:val="15"/></w:compat>
</w:settings>`
