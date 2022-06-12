# paste

## share something

<style>
  textarea {
    margin-top: 1rem;
    grid-column: 1 / span 5;
    height: 70vh;
  }
</style>

<textarea
id="paste"
name="paste"
form="form"
autofocus
placeholder="paste something here..."
rows="10"
cols="40"

> </textarea>

<form
  id="form"
  autocomplete="off"
  action="/paste/"
  enctype="multipart/form-data"
  method="POST"
>
  <label>
    Or upload:
    <input type="file" id="upload" name="upload" form="form" />
  </label>

  <input type="submit" id="submit" value="Send" form="form" />
</form>
