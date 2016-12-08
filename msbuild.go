package msbuild

import (
  "fmt"
  "io"
  "os"
  "path/filepath"
  "github.com/google/uuid"
  "github.com/kuma777/go-msbuild/xml"
)

var (
  UUIDSPACE string = "10758f2f-f8bc-4d6b-aeaa-8131bf78a862" // Your UUID Space Here
)

type TemplateCallback func(element *Element)

type Element struct {
  name        xml.Name
  attributes  []xml.Attr
  children    []interface{}
}

func (e *Element) AddAttribute(name, value string) {
  var attr xml.Attr
  attr.Name.Local = name
  attr.Value = value
  e.attributes = append(e.attributes, attr)
}

func (e *Element) AddChild(name string) *Element {
  child := &Element{}
  child.name.Local = name
  e.children = append(e.children, child)
  return child
}

func (e *Element) AddCharData(value string) {
  child := xml.CharData(value)
  e.children = append(e.children, child)
}

func (in *Element) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
  start.Name = in.name
  start.Attr = in.attributes
  e.EncodeToken(start)
  for _, child := range in.children {
    switch child.(type) {
    case *Element:
      c := child.(*Element)
      err := e.Encode(c)
      if err != nil {
        return err
      }
    case xml.CharData:
      e.EncodeToken(child.(xml.CharData))
    case xml.Comment:
      e.EncodeToken(child.(xml.Comment))
    }
  }
  e.EncodeToken(start.End())
  return nil
}

func (out *Element) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
  out.name        = start.Name
  out.name.Space  = ""
  out.attributes  = start.Attr

  for {
    token, err := d.Token()
    if err != nil {
      if err == io.EOF {
        return nil
      }
      return err
    }

    switch token.(type) {
    case xml.StartElement:
      var element *Element
      t := token.(xml.StartElement)
      err := d.DecodeElement(&element, &t)
      if err != nil {
        return err
      }
      out.children = append(out.children, element)
    case xml.CharData:
      out.children = append(out.children, token.(xml.CharData).Copy())
    case xml.Comment:
      out.children = append(out.children, token.(xml.Comment).Copy())
    }
  }
}

func scanTemplate(element *Element, callback TemplateCallback) {
  if element.name.Local == "ItemGroup" {
    callback(element)
  } else {
    for _, child := range element.children {
      switch child.(type) {
      case *Element:
        scanTemplate(child.(*Element), callback)
      default:
        // NO-OP
      }
    }
  }
}

func overrideSources(element *Element, files []string) {
  element.attributes = element.attributes[:0]

  for _, file := range files {
    file = filepath.FromSlash(file)
    ext := filepath.Ext(file)
    if ext != ".cpp" && ext != ".cxx" {
      continue
    }

    child := element.AddChild("ClCompile")
    child.AddAttribute("Include", file)

    element.AddCharData("\n")
  }
}

func overrideHeaders(element *Element, files []string) {
  element.attributes = element.attributes[:0]

  for _, file := range files {
    file = filepath.FromSlash(file)
    ext := filepath.Ext(file)
    if ext != ".h" {
      continue
    }

    child := element.AddChild("ClInclude")
    child.AddAttribute("Include", file)

    element.AddCharData("\n")
  }
}

func ExportProject(files []string, outdir, projname string) {
  exepath, err := filepath.Abs(filepath.Dir(os.Args[0]))
  if err != nil {
    fmt.Println("An error occurred while getting executable path.")
    return
  }

  fp_in, err := os.OpenFile(filepath.Join(exepath, "template.vcxproj"), os.O_RDONLY, 0666)
  if err != nil {
    fmt.Println("File opening error occurred while reading project template.")
    return
  }

  element := &Element{}
  xml.NewDecoder(fp_in).Decode(&element)

  fp_in.Close()

  fn := func(element *Element) {
    for _, attr := range element.attributes {
      if attr.Name.Local == "Label" {
        switch attr.Value {
        case "Sources":
          overrideSources(element, files)
        case "Headers":
          overrideHeaders(element, files)
        }
      }
    }
  }

  scanTemplate(element, fn)

  outpath := filepath.Join(filepath.ToSlash(outdir), projname + ".vcxproj")

  fp_out, err := os.OpenFile(outpath, os.O_CREATE | os.O_TRUNC, 0666)
  if err != nil {
    fmt.Println("File opening error occurred while writing project file.")
    return
  }
  xml.NewEncoder(fp_out).Encode(element)

  fp_out.Close()

  exportFilter(files, outpath + ".filters")
}

func exportFilter(files []string, outpath string) {
  space, err := uuid.Parse(UUIDSPACE)
  if err != nil {
    return
  }

  root, err := filepath.Abs(filepath.Dir("./"))
  if err != nil {
    fmt.Println("An error occurred while getting root path.")
    return
  }

  project := &Element{}
  project.name.Local = "Project"
  project.name.Space = "http://schemas.microsoft.com/developer/msbuild/2003"
  project.AddAttribute("ToolsVersion", "4.0")

  elmFilters := project.AddChild("ItemGroup")
  elmEntries := project.AddChild("ItemGroup")

  for _, file := range files {
    file = filepath.FromSlash(file)
    dir, err := filepath.Rel(root, filepath.Dir(file))
    if err != nil {
      continue
    }

    ext := filepath.Ext(file)
    name := ""
    tag := ""
    if ext == ".h" {
      name = filepath.Join("Header Files", dir)
      tag = "ClInclude"
    } else {
      name = filepath.Join("Source Files", dir)
      tag = "ClCompile"
    }

    child := elmEntries.AddChild(tag)
    child.AddAttribute("Include", file)
    filter := child.AddChild("Filter")
    filter.AddCharData(name)

    path := name
    for {
      filterExists := false
      for _, elm := range elmFilters.children {
        value := elm.(*Element).attributes[0].Value
        if path == value {
          filterExists = true
          break
        }
      }

      if !filterExists {
        idstr := uuid.NewSHA1(space, []byte(path)).String()

        child := elmFilters.AddChild("Filter")
        child.AddAttribute("Include", path)
        id := child.AddChild("UniqueIdentifier")
        id.AddCharData("{" + idstr + "}")
      }

      path, _ = filepath.Split(path)

      if len(path) == 0 {
        break
      }

      path = path[0:len(path) - 1]
    }
  }

  fp_filter, err := os.OpenFile(outpath, os.O_CREATE | os.O_TRUNC, 0666)
  if err != nil {
    fmt.Println("File opening error occurred while writing filters.")
    return
  }
  enc := xml.NewEncoder(fp_filter)
  enc.Indent("", "  ")
  enc.Encode(project)

  fp_filter.Close()
}
