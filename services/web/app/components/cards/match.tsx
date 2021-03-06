import * as React from "react";
import { Card, Icon, Button, Label, Divider, Grid } from "semantic-ui-react"

export class MatchCard extends React.Component<{}, {}> {
  render() {
    return (
      <Card>
        <Card.Content>
          <Versus />
        </Card.Content>
        <Button attached="bottom">
          <Icon name="eye" />
          Watch
        </Button>
      </Card>
    );
  }
}

export class Versus extends React.Component<{}, {}> {
  render() {
    return (
    <div style={{position: "relative"}}>
      <Grid textAlign="center">
        <Grid.Column width={8}>
          <Icon size="large" color="yellow" name="trophy" />
          <div>
            SuperB0t
          </div>
          <div className="small">Rank 1</div>
        </Grid.Column>
        <Divider vertical>vs</Divider>
        <Grid.Column width={8}>
          <Icon size="large" color="red" name="frown" />
          <div>
            Betterbotter
          </div>
          <div className="small">Rank 2</div>
        </Grid.Column>
      </Grid>
   </div>
    );
  }
}




